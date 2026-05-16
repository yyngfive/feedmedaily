package metadata

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/logging"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

var (
	doiPattern         = regexp.MustCompile(`10\.\d{4,9}/[-._;()/:A-Z0-9]+`)
	openAlexBaseURL    = "https://api.openalex.org"
	crossrefBaseURL    = "https://api.crossref.org"
	metadataHTTPClient = &http.Client{Timeout: 15 * time.Second}
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

func EnrichPaper(paper store.Paper) store.Paper {
	// 只迁 reclassify 所需的 metadata enrich，失败时走本地内容回退。
	normalizedDOI := NormalizeDOI(stringValue(paper.DOI))
	externalErrors := []string{}
	if (paper.AbstractSource == "openalex" || paper.AbstractSource == "crossref") && paper.Abstract != nil && paper.Journal != nil {
		result := paper
		if normalizedDOI != "" {
			result.DOI = stringPtr(normalizedDOI)
		}
		logEnrichmentResult(result, normalizedDOI, externalErrors)
		return result
	}

	enriched := paper
	if normalizedDOI != "" {
		enriched.DOI = stringPtr(normalizedDOI)
	}

	if openAlexPaper, ok, errText := enrichWithOpenAlex(enriched); ok && strings.TrimSpace(stringValue(openAlexPaper.Abstract)) != "" {
		result := finalizeEnrichedPaper(paper, openAlexPaper, "openalex")
		logEnrichmentResult(result, normalizedDOI, externalErrors)
		return result
	} else if errText != "" {
		externalErrors = append(externalErrors, "openalex:"+errText)
	}
	if crossrefPaper, ok, errText := enrichWithCrossref(enriched); ok && strings.TrimSpace(stringValue(crossrefPaper.Abstract)) != "" {
		result := finalizeEnrichedPaper(paper, crossrefPaper, "crossref")
		logEnrichmentResult(result, normalizedDOI, externalErrors)
		return result
	} else if errText != "" {
		externalErrors = append(externalErrors, "crossref:"+errText)
	}
	if (paper.AbstractSource == "openalex" || paper.AbstractSource == "crossref") && paper.Abstract != nil {
		result := paper
		if normalizedDOI != "" {
			result.DOI = stringPtr(normalizedDOI)
		}
		logEnrichmentResult(result, normalizedDOI, externalErrors)
		return result
	}
	if hasAbstractContent(paper) {
		result := finalizeEnrichedPaper(paper, enriched, "rss")
		logEnrichmentResult(result, normalizedDOI, externalErrors)
		return result
	}
	result := finalizeEnrichedPaper(paper, enriched, "none")
	logEnrichmentResult(result, normalizedDOI, externalErrors)
	return result
}

func finalizeEnrichedPaper(original store.Paper, candidate store.Paper, source string) store.Paper {
	journal := firstNonEmpty(stringValue(candidate.Journal), stringValue(original.Journal))
	doi := NormalizeDOI(firstNonEmpty(stringValue(candidate.DOI), stringValue(original.DOI)))
	result := original
	if doi != "" {
		result.DOI = stringPtr(doi)
	}
	if journal != "" {
		result.Journal = stringPtr(journal)
	}
	switch source {
	case "openalex", "crossref":
		result.Abstract = candidate.Abstract
		result.AbstractHTML = nil
		result.AbstractImages = []store.AbstractImage{}
		if candidate.Abstract != nil && strings.TrimSpace(*candidate.Abstract) != "" {
			result.AbstractSource = source
		} else {
			result.AbstractSource = "none"
		}
		return result
	case "rss":
		if hasAbstractContent(original) {
			result.AbstractSource = "rss"
			return result
		}
	}
	result.Abstract = nil
	result.AbstractHTML = nil
	result.AbstractImages = []store.AbstractImage{}
	result.AbstractSource = "none"
	return result
}

func enrichWithOpenAlex(paper store.Paper) (store.Paper, bool, string) {
	var url string
	if doi := NormalizeDOI(stringValue(paper.DOI)); doi != "" {
		url = strings.TrimRight(openAlexBaseURL, "/") + "/works/https://doi.org/" + doi
	} else {
		url = strings.TrimRight(openAlexBaseURL, "/") + "/works?search=" + queryEscape(paper.Title) + "&per-page=1"
	}
	responseBody, ok, errText := httpGet(url)
	if !ok {
		return paper, false, errText
	}
	var payload map[string]any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return paper, false, err.Error()
	}
	work := payload
	if results, ok := payload["results"].([]any); ok && len(results) > 0 {
		if first, ok := results[0].(map[string]any); ok {
			work = first
		}
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
	result := paper
	if doi != "" {
		result.DOI = stringPtr(doi)
	}
	if journal != "" {
		result.Journal = stringPtr(journal)
	}
	if strings.TrimSpace(abstract) != "" {
		result.Abstract = stringPtr(abstract)
		result.AbstractSource = "openalex"
	} else {
		result.Abstract = nil
		result.AbstractSource = "none"
	}
	return result, true, ""
}

func enrichWithCrossref(paper store.Paper) (store.Paper, bool, string) {
	doi := NormalizeDOI(stringValue(paper.DOI))
	if doi == "" {
		return paper, false, ""
	}
	responseBody, ok, errText := httpGet(strings.TrimRight(crossrefBaseURL, "/") + "/works/" + doi)
	if !ok {
		return paper, false, errText
	}
	var payload map[string]any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return paper, false, err.Error()
	}
	message, ok := payload["message"].(map[string]any)
	if !ok {
		return paper, false, "crossref response missing message object"
	}
	journal := stringValue(paper.Journal)
	if titles, ok := message["container-title"].([]any); ok && len(titles) > 0 {
		journal = firstNonEmpty(journal, normalizedString(titles[0]))
	}
	abstract := normalizedString(message["abstract"])
	result := paper
	result.DOI = stringPtr(doi)
	if journal != "" {
		result.Journal = stringPtr(journal)
	}
	if abstract != "" {
		result.Abstract = stringPtr(abstract)
		result.AbstractSource = "crossref"
	} else {
		result.Abstract = nil
		result.AbstractSource = "none"
	}
	return result, true, ""
}

func hasAbstractContent(paper store.Paper) bool {
	return paper.Abstract != nil || paper.AbstractHTML != nil || len(paper.AbstractImages) > 0
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

func logEnrichmentResult(paper store.Paper, normalizedDOI string, externalErrors []string) {
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "metadata",
		Action:    "abstract_enrichment",
		Message:   "abstract_enrichment",
		Data: map[string]any{
			"paper_key":        PaperKey(paper),
			"doi_found":        normalizedDOI != "",
			"abstract_source":  paper.AbstractSource,
			"abstract_empty":   !hasAbstractContent(paper),
			"external_errors":  externalErrors,
			"journal_resolved": strings.TrimSpace(stringValue(paper.Journal)) != "",
		},
	})
}
