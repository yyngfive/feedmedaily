package metadata

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yyngfive/scirssagent/internal/logging"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

var (
	doiPattern         = regexp.MustCompile(`10\.\d{4,9}/[-._;()/:A-Z0-9]+`)
	openAlexBaseURL    = "https://api.openalex.org"
	crossrefBaseURL    = "https://api.crossref.org"
	metadataHTTPClient = &http.Client{Timeout: 15 * time.Second}
	tagRE              = regexp.MustCompile(`<[^>]+>`)
	whitespaceRE       = regexp.MustCompile(`\s+`)
	abstractPrefixRE   = regexp.MustCompile(`(?i)^(abstract|summary)\s*:?`)
	nonAlnumRE         = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	digitsRE           = regexp.MustCompile(`^\d+$`)
)

const (
	// errDOIRejected 表示按 DOI 精确查询到的记录与论文的标题、发布日期
	// 没有双双一致，即该 DOI 无法确认指向这篇论文。
	errDOIRejected = "doi-rejected"
	// errNoMatchingResult 表示标题搜索返回的候选里没有一条能同时通过
	// 标题与发布日期校验。
	errNoMatchingResult = "no-matching-result"
)

func NormalizeDOI(value string) string {
	// 复刻 Python DOI 清洗逻辑，优先提取标准 DOI 片段。
	clean := strings.TrimSpace(value)
	if clean == "" {
		return ""
	}
	match := doiPattern.FindString(strings.ToUpper(clean))
	if match == "" {
		return strings.TrimSpace(strings.TrimPrefix(strings.ToLower(clean), "doi:"))
	}
	return strings.TrimSuffix(strings.ToLower(match), ".")
}

func PaperKey(paper store.Paper) string {
	// 与 Python 一致地按 DOI > URL > title 生成稳定键。
	doi := NormalizeDOI(stringValue(paper.DOI))
	if doi != "" {
		return "doi:" + doi
	}
	if strings.TrimSpace(paper.URL) != "" {
		return "url:" + strings.ToLower(strings.TrimSpace(paper.URL))
	}
	return "title:" + strings.ToLower(strings.TrimSpace(paper.Title))
}

func AbstractFromOpenAlexInvertedIndex(index map[string][]int) string {
	// 把 OpenAlex 倒排摘要还原成纯文本摘要。
	type positionedWord struct {
		Position int
		Word     string
	}
	words := make([]positionedWord, 0)
	for word, positions := range index {
		for _, position := range positions {
			words = append(words, positionedWord{Position: position, Word: word})
		}
	}
	for i := 0; i < len(words); i++ {
		for j := i + 1; j < len(words); j++ {
			if words[j].Position < words[i].Position {
				words[i], words[j] = words[j], words[i]
			}
		}
	}
	parts := make([]string, 0, len(words))
	for _, item := range words {
		parts = append(parts, item.Word)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// EnrichPaper 只做 reclassify 所需的 metadata enrich，失败时走本地内容回退。
// 第二个返回值表示论文自带的 DOI 被校验判定为错配（两个信号都对不上）而丢弃。
func EnrichPaper(paper store.Paper) (store.Paper, bool) {
	normalizedDOI := NormalizeDOI(stringValue(paper.DOI))
	hadDOIAtStart := normalizedDOI != ""
	externalErrors := []string{}
	if !needsExternalEnrichment(paper) {
		result := paper
		if normalizedDOI != "" {
			result.DOI = stringPtr(normalizedDOI)
		}
		logEnrichmentResult(result, externalErrors)
		return result, false
	}

	enriched := paper
	if normalizedDOI != "" {
		enriched.DOI = stringPtr(normalizedDOI)
	}

	if openAlexPaper, errText := enrichWithOpenAlex(enriched); openAlexPaper != nil {
		enriched = applyMetadataCandidate(enriched, *openAlexPaper)
	} else if errText != "" {
		externalErrors = append(externalErrors, "openalex:"+errText)
	}
	doiRejected := false
	if hadDOIAtStart {
		if crossrefPaper, errText := enrichWithCrossref(enriched); crossrefPaper != nil {
			enriched = applyMetadataCandidate(enriched, *crossrefPaper)
		} else if errText == errDOIRejected {
			// Crossref 是 DOI 注册机构，其记录无法同时通过标题与发布日期
			// 校验时，判定该 DOI 不可信：丢弃 DOI，链接回退到出版社 URL。
			doiRejected = true
			externalErrors = append(externalErrors, "crossref:"+errText)
		} else if errText != "" {
			externalErrors = append(externalErrors, "crossref:"+errText)
		}
	}
	if doiRejected {
		enriched.DOI = nil
	}
	if !hasAbstractContent(enriched) {
		enriched.Abstract = nil
		enriched.AbstractHTML = nil
		enriched.AbstractImages = []store.AbstractImage{}
		enriched.AbstractSource = "none"
	}
	logEnrichmentResult(enriched, externalErrors)
	return enriched, doiRejected
}

func needsExternalEnrichment(paper store.Paper) bool {
	if strings.TrimSpace(stringValue(paper.DOI)) == "" {
		return true
	}
	if strings.TrimSpace(stringValue(paper.Journal)) == "" {
		return true
	}
	if len(paper.Authors) == 0 {
		return true
	}
	return !hasAbstractContent(paper)
}

func enrichWithOpenAlex(paper store.Paper) (*store.Paper, string) {
	var url string
	searchMode := NormalizeDOI(stringValue(paper.DOI)) == ""
	if searchMode {
		// 没有 DOI 时按标题搜索；标题搜索的第一条结果经常是同名近似论文，
		// 因此取前 5 条并做标题/日期校验，逐条找到第一条能对应当前论文的记录。
		url = strings.TrimRight(openAlexBaseURL, "/") + "/works?search=" + queryEscape(paper.Title) + "&per-page=5&select=doi,title,publication_date,publication_year,primary_location,authorships,abstract_inverted_index"
	} else {
		url = strings.TrimRight(openAlexBaseURL, "/") + "/works/https://doi.org/" + NormalizeDOI(stringValue(paper.DOI))
	}
	responseBody, ok, errText := httpGet(url)
	if !ok {
		return nil, errText
	}
	var payload map[string]any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return nil, err.Error()
	}
	work := payload
	if searchMode {
		results, _ := payload["results"].([]any)
		matched := false
		for _, rawResult := range results {
			result, ok := rawResult.(map[string]any)
			if !ok {
				continue
			}
			if externalRecordMatches(paper, openAlexRecordTitle(result), openAlexRecordDate(result)) {
				work = result
				matched = true
				break
			}
		}
		if !matched {
			return nil, errNoMatchingResult
		}
	} else if !externalRecordMatches(paper, openAlexRecordTitle(work), openAlexRecordDate(work)) {
		return nil, errDOIRejected
	}
	abstract := ""
	if rawIndex, ok := work["abstract_inverted_index"].(map[string]any); ok {
		index := map[string][]int{}
		for word, rawPositions := range rawIndex {
			values := []int{}
			if positions, ok := rawPositions.([]any); ok {
				for _, rawPosition := range positions {
					if value, ok := rawPosition.(float64); ok {
						values = append(values, int(value))
					}
				}
			}
			index[word] = values
		}
		abstract = AbstractFromOpenAlexInvertedIndex(index)
	}
	doi := NormalizeDOI(firstNonEmpty(stringValue(paper.DOI), normalizedString(work["doi"])))
	journal := stringValue(paper.Journal)
	if primaryLocation, ok := work["primary_location"].(map[string]any); ok {
		if source, ok := primaryLocation["source"].(map[string]any); ok {
			journal = firstNonEmpty(journal, normalizedString(source["display_name"]))
		}
	}
	authors := openAlexAuthors(work)
	result := paper
	if doi != "" {
		result.DOI = stringPtr(doi)
	}
	if journal != "" {
		result.Journal = stringPtr(journal)
	}
	if len(authors) > 0 {
		result.Authors = authors
	}
	if strings.TrimSpace(abstract) != "" {
		result.Abstract = stringPtr(abstract)
		result.AbstractSource = "openalex"
	} else {
		result.Abstract = nil
		result.AbstractSource = "none"
	}
	return &result, ""
}

func openAlexRecordTitle(work map[string]any) string {
	return firstNonEmpty(normalizedString(work["title"]), normalizedString(work["display_name"]))
}

func openAlexRecordDate(work map[string]any) string {
	return firstNonEmpty(normalizedString(work["publication_date"]), normalizedString(work["publication_year"]))
}

func enrichWithCrossref(paper store.Paper) (*store.Paper, string) {
	doi := NormalizeDOI(stringValue(paper.DOI))
	if doi == "" {
		return nil, ""
	}
	responseBody, ok, errText := httpGet(strings.TrimRight(crossrefBaseURL, "/") + "/works/" + doi)
	if !ok {
		return nil, errText
	}
	var payload map[string]any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return nil, err.Error()
	}
	message, ok := payload["message"].(map[string]any)
	if !ok {
		return nil, "crossref response missing message object"
	}
	if !externalRecordMatches(paper, crossrefRecordTitle(message), crossrefRecordDate(message)) {
		return nil, errDOIRejected
	}
	journal := stringValue(paper.Journal)
	if titles, ok := message["container-title"].([]any); ok && len(titles) > 0 {
		journal = firstNonEmpty(journal, normalizedString(titles[0]))
	}
	abstract := sanitizeExternalAbstract(normalizedString(message["abstract"]))
	authors := crossrefAuthors(message)
	result := paper
	result.DOI = stringPtr(doi)
	if journal != "" {
		result.Journal = stringPtr(journal)
	}
	if len(authors) > 0 {
		result.Authors = authors
	}
	if abstract != "" {
		result.Abstract = stringPtr(abstract)
		result.AbstractSource = "crossref"
	} else {
		result.Abstract = nil
		result.AbstractSource = "none"
	}
	return &result, ""
}

func crossrefRecordTitle(message map[string]any) string {
	titles, ok := message["title"].([]any)
	if !ok || len(titles) == 0 {
		return ""
	}
	return normalizedString(titles[0])
}

func crossrefRecordDate(message map[string]any) string {
	issued, ok := message["issued"].(map[string]any)
	if !ok {
		return ""
	}
	dateParts, ok := issued["date-parts"].([]any)
	if !ok || len(dateParts) == 0 {
		return ""
	}
	parts, ok := dateParts[0].([]any)
	if !ok || len(parts) == 0 {
		return ""
	}
	year := normalizedString(parts[0])
	if len(parts) > 1 {
		month := normalizedString(parts[1])
		if len(month) == 1 {
			month = "0" + month
		}
		return year + "-" + month
	}
	return year
}

// externalRecordMatches 判断外部记录是否对应当前论文：标题与发布日期必须
// 双双一致才采信（只凭标题一致不足以确认，避免标题近似的无关论文被采纳）；
// 任一项对不上都判定为错配。RSS 标题可能被截断或带 HTML 标记，因此标题
// 匹配用归一化后的相等或包含关系；日期按年月比较。
func externalRecordMatches(paper store.Paper, recordTitle string, recordDate string) bool {
	return titlesMatch(paper.Title, recordTitle) && datesMatch(stringValue(paper.PublishedDate), recordDate)
}

func titlesMatch(paperTitle string, recordTitle string) bool {
	left := normalizeTitle(paperTitle)
	right := normalizeTitle(recordTitle)
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	// 包含判定要求较短一方足够长，避免短标题误配。
	if utf8.RuneCountInString(left) >= 20 && strings.Contains(right, left) {
		return true
	}
	if utf8.RuneCountInString(right) >= 20 && strings.Contains(left, right) {
		return true
	}
	return false
}

func normalizeTitle(value string) string {
	clean := html.UnescapeString(value)
	clean = tagRE.ReplaceAllString(clean, " ")
	clean = strings.ToLower(clean)
	clean = nonAlnumRE.ReplaceAllString(clean, " ")
	return strings.Join(strings.Fields(clean), " ")
}

// datesMatch 按年月比较发布日期；任一方只有年份时退化为按年比较。
func datesMatch(paperDate string, recordDate string) bool {
	paperYear, paperMonth := parseYearMonth(paperDate)
	recordYear, recordMonth := parseYearMonth(recordDate)
	if paperYear == "" || recordYear == "" {
		return false
	}
	if paperYear != recordYear {
		return false
	}
	if paperMonth == "" || recordMonth == "" {
		return true
	}
	return paperMonth == recordMonth
}

func parseYearMonth(value string) (string, string) {
	clean := strings.TrimSpace(value)
	if len(clean) < 4 || !digitsRE.MatchString(clean[:4]) {
		return "", ""
	}
	year := clean[:4]
	month := ""
	if len(clean) >= 7 && clean[4] == '-' && digitsRE.MatchString(clean[5:7]) {
		month = clean[5:7]
	}
	return year, month
}

func hasAbstractContent(paper store.Paper) bool {
	return paper.Abstract != nil || paper.AbstractHTML != nil || len(paper.AbstractImages) > 0
}

func applyMetadataCandidate(base store.Paper, candidate store.Paper) store.Paper {
	result := base
	doi := NormalizeDOI(firstNonEmpty(stringValue(candidate.DOI), stringValue(base.DOI)))
	if doi != "" {
		result.DOI = stringPtr(doi)
	}
	if journal := firstNonEmpty(stringValue(candidate.Journal), stringValue(base.Journal)); journal != "" {
		result.Journal = stringPtr(journal)
	}
	if len(result.Authors) == 0 && len(candidate.Authors) > 0 {
		result.Authors = append([]string{}, candidate.Authors...)
	}
	if shouldReplaceAbstract(result, candidate) {
		result.Abstract = candidate.Abstract
		result.AbstractHTML = nil
		result.AbstractImages = []store.AbstractImage{}
		result.AbstractSource = candidate.AbstractSource
	}
	return result
}

func shouldReplaceAbstract(existing store.Paper, candidate store.Paper) bool {
	if !hasAbstractContent(candidate) {
		return false
	}
	if !hasAbstractContent(existing) {
		return true
	}
	return abstractSourcePriority(candidate.AbstractSource) >= abstractSourcePriority(existing.AbstractSource)
}

func abstractSourcePriority(source string) int {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "rss":
		return 1
	case "crossref":
		return 2
	case "openalex":
		return 3
	default:
		return 0
	}
}

func crossrefAuthors(message map[string]any) []string {
	rawAuthors, ok := message["author"].([]any)
	if !ok {
		return nil
	}
	authors := make([]string, 0, len(rawAuthors))
	for _, rawAuthor := range rawAuthors {
		author, ok := rawAuthor.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(strings.Join([]string{
			normalizedString(author["given"]),
			normalizedString(author["family"]),
		}, " "))
		if name == "" {
			name = normalizedString(author["name"])
		}
		if name == "" {
			continue
		}
		authors = append(authors, whitespaceRE.ReplaceAllString(name, " "))
	}
	return authors
}

func openAlexAuthors(work map[string]any) []string {
	rawAuthorships, ok := work["authorships"].([]any)
	if !ok {
		return nil
	}
	authors := make([]string, 0, len(rawAuthorships))
	for _, rawAuthorship := range rawAuthorships {
		authorship, ok := rawAuthorship.(map[string]any)
		if !ok {
			continue
		}
		author, ok := authorship["author"].(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(normalizedString(author["display_name"]))
		if name == "" {
			continue
		}
		authors = append(authors, whitespaceRE.ReplaceAllString(name, " "))
	}
	return authors
}

func sanitizeExternalAbstract(raw string) string {
	clean := strings.TrimSpace(html.UnescapeString(raw))
	if clean == "" {
		return ""
	}
	clean = strings.TrimSpace(tagRE.ReplaceAllString(clean, " "))
	clean = strings.TrimSpace(whitespaceRE.ReplaceAllString(clean, " "))
	clean = strings.TrimSpace(abstractPrefixRE.ReplaceAllString(clean, ""))
	return clean
}

func httpGet(url string) ([]byte, bool, string) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err.Error()
	}
	request.Header.Set("User-Agent", "SciRSSAgent/0.1")
	started := time.Now()
	response, err := metadataHTTPClient.Do(request)
	if err != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "error",
			Component: "metadata",
			Action:    "http_request_failed",
			Message:   fmt.Sprintf("HTTP Request: GET %s failed", url),
			Error:     err.Error(),
			Data: map[string]any{
				"url":         url,
				"duration_ms": time.Since(started).Milliseconds(),
			},
		})
		return nil, false, err.Error()
	}
	defer response.Body.Close()
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "metadata",
		Action:    "http_request",
		Message:   fmt.Sprintf("HTTP Request: GET %s %q", url, response.Proto+" "+response.Status),
		Data: map[string]any{
			"url":         url,
			"status_code": response.StatusCode,
			"duration_ms": time.Since(started).Milliseconds(),
		},
	})
	if response.StatusCode != http.StatusOK {
		return nil, false, response.Status
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, false, err.Error()
	}
	return body, true, ""
}

func queryEscape(value string) string {
	replacer := strings.NewReplacer("%", "%25", " ", "%20", "\"", "%22", "#", "%23", "&", "%26", "+", "%2B", "?", "%3F")
	return replacer.Replace(value)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPtr(value string) *string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return nil
	}
	return &clean
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizedString(raw any) string {
	value := strings.TrimSpace(fmt.Sprintf("%v", raw))
	if value == "<nil>" {
		return ""
	}
	return value
}

func logEnrichmentResult(paper store.Paper, externalErrors []string) {
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "metadata",
		Action:    "abstract_enrichment",
		Message:   "abstract_enrichment",
		Data: map[string]any{
			"paper_key":        PaperKey(paper),
			"doi_found":        strings.TrimSpace(stringValue(paper.DOI)) != "",
			"abstract_source":  paper.AbstractSource,
			"abstract_empty":   !hasAbstractContent(paper),
			"external_errors":  externalErrors,
			"journal_resolved": strings.TrimSpace(stringValue(paper.Journal)) != "",
		},
	})
}
