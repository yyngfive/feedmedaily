package feeds

import (
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/logging"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

type FetchOptions struct {
	MaxPapers int
}

type FetchResult struct {
	Papers   []store.Paper
	Errors   []string
	Fetched  int
	FeedURLs []string
}

type FeedError struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

var (
	fetchHTTPClient       = &http.Client{Timeout: 30 * time.Second}
	fetchRetryBackoffs    = []time.Duration{200 * time.Millisecond, 600 * time.Millisecond}
	whitespaceRE          = regexp.MustCompile(`\s+`)
	tagRE                 = regexp.MustCompile(`<[^>]+>`)
	imgSrcRE              = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)
	metadataRE            = regexp.MustCompile(`(?i)\b(vol(?:ume)?|issue|pp?\.|pages?|doi|e?issn|published|online)\b`)
	abstractHeadingRE     = regexp.MustCompile(`(?i)(?:^|\s)ABSTRACT[:\s-]*`)
	naturePrefixRE        = regexp.MustCompile(`(?i)^[^.]*?,\s*Published online:\s*.*?;\s*doi:\S+\s*`)
	doiValueRE            = regexp.MustCompile(`10\.\d{4,9}/[-._;()/:A-Z0-9]+`)
	feedXMLPrefixRE       = regexp.MustCompile(`(?is)^\s*(?:<\?xml\b[^>]*>\s*)?<(?:rss|rdf:RDF|feed)\b`)
	feedHTMLPrefixRE      = regexp.MustCompile(`(?is)^\s*(?:<!doctype\s+html\b\s*>)?\s*<html\b`)
	feedChallengeMarkerRE = regexp.MustCompile(`(?is)(just a moment|enable javascript and cookies|attention required|__cf_chl_|cf-browser-verification|challenge-platform)`)
)

type rssDoc struct {
	Channel rssChannel `xml:"channel"`
	Items   []rssItem  `xml:"item"`
}

type rssChannel struct {
	Title string    `xml:"title"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title    string `xml:"title"`
	Link     string `xml:"link"`
	GUID     string `xml:"guid"`
	Author   string `xml:"author"`
	PubDate  string `xml:"pubDate"`
	InnerXML string `xml:",innerxml"`
}

type atomDoc struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title         string       `xml:"title"`
	ID            string       `xml:"id"`
	Summary       string       `xml:"summary,innerxml"`
	SummaryPlain  string       `xml:"summary"`
	Content       string       `xml:"content,innerxml"`
	DCDescription string       `xml:"http://purl.org/dc/elements/1.1/ description"`
	Published     string       `xml:"published"`
	Updated       string       `xml:"updated"`
	DCSource      string       `xml:"http://purl.org/dc/elements/1.1/ source"`
	PrismPubName  string       `xml:"http://prismstandard.org/namespaces/basic/2.0/ publicationName"`
	Authors       []atomAuthor `xml:"author"`
	Links         []atomLink   `xml:"link"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
}

type feedRootProbe struct {
	XMLName xml.Name
	Channel *struct{}  `xml:"channel"`
	Items   []struct{} `xml:"item"`
}

// FetchAll reads configured feeds, fetches them, and normalizes entries into paper candidates.
func FetchAll(feedsPath string, opts FetchOptions) (FetchResult, error) {
	subscriptions, err := ReadSubscriptions(feedsPath)
	if err != nil {
		return FetchResult{}, err
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "feeds",
		Action:    "fetch_all_started",
		Message:   fmt.Sprintf("Fetching %d feeds", len(subscriptions)),
	})
	result := FetchResult{
		Papers:   []store.Paper{},
		Errors:   []string{},
		Fetched:  0,
		FeedURLs: make([]string, 0, len(subscriptions)),
	}
	for _, subscription := range subscriptions {
		result.FeedURLs = append(result.FeedURLs, subscription.URL)
		papers, err := fetchFeed(subscription.URL)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", subscription.URL, err))
			continue
		}
		result.Papers = append(result.Papers, papers...)
	}
	if opts.MaxPapers > 0 && len(result.Papers) > opts.MaxPapers {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "info",
			Component: "feeds",
			Action:    "fetch_all_limited",
			Message:   fmt.Sprintf("Limiting run to %d papers", opts.MaxPapers),
		})
		result.Papers = result.Papers[:opts.MaxPapers]
	}
	result.Fetched = len(result.Papers)
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "feeds",
		Action:    "fetch_all_completed",
		Message:   fmt.Sprintf("Fetched %d candidate papers", result.Fetched),
		Data: map[string]any{
			"feed_count": len(subscriptions),
			"errors":     len(result.Errors),
		},
	})
	return result, nil
}

func fetchFeed(url string) ([]store.Paper, error) {
	attemptCount := len(fetchRetryBackoffs) + 1
	lastStatusCode := 0
	lastChallenge := false
	var lastErr error
	for attempt := 1; attempt <= attemptCount; attempt++ {
		papers, statusCode, challenge, retryable, err := fetchFeedAttempt(url, attempt)
		if err == nil {
			return papers, nil
		}
		lastStatusCode = statusCode
		lastChallenge = challenge
		lastErr = err
		if !retryable || attempt == attemptCount {
			break
		}
		time.Sleep(fetchRetryBackoffs[attempt-1])
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "warning",
		Component: "feeds",
		Action:    "feed_fetch_failed",
		Message:   fmt.Sprintf("Failed to fetch feed after %d attempt(s): %s", attemptCount, url),
		Error:     lastErr.Error(),
		Data: map[string]any{
			"url":                 url,
			"attempts":            attemptCount,
			"last_status_code":    lastStatusCode,
			"challenge_suspected": lastChallenge,
		},
	})
	return nil, lastErr
}

func fetchFeedAttempt(url string, attempt int) ([]store.Paper, int, bool, bool, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, false, false, err
	}
	applyFeedRequestHeaders(request)
	started := time.Now()
	response, err := fetchHTTPClient.Do(request)
	if err != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "error",
			Component: "feeds",
			Action:    "http_request_failed",
			Message:   fmt.Sprintf("HTTP Request: GET %s failed", url),
			Error:     err.Error(),
			Data: map[string]any{
				"url":            url,
				"attempt":        attempt,
				"duration_ms":    time.Since(started).Milliseconds(),
				"request_method": http.MethodGet,
			},
		})
		return nil, 0, false, true, err
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "feeds",
		Action:    "http_request",
		Message:   fmt.Sprintf("HTTP Request: GET %s %q", url, response.Proto+" "+response.Status),
		Data: map[string]any{
			"url":            url,
			"attempt":        attempt,
			"status_code":    response.StatusCode,
			"duration_ms":    time.Since(started).Milliseconds(),
			"request_method": http.MethodGet,
		},
	})
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		retryable := isRetryableFeedStatus(response.StatusCode)
		return nil, response.StatusCode, false, retryable, fmt.Errorf("request failed with %s", response.Status)
	}
	body, err := ioReadAll(response)
	if err != nil {
		return nil, response.StatusCode, false, true, err
	}
	if challenge := looksLikeChallengeResponse(body); challenge {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "warning",
			Component: "feeds",
			Action:    "feed_challenge_suspected",
			Message:   fmt.Sprintf("Feed returned challenge-like HTML instead of XML: %s", url),
			Data: map[string]any{
				"url":                 url,
				"attempt":             attempt,
				"status_code":         response.StatusCode,
				"challenge_suspected": true,
			},
		})
		return nil, response.StatusCode, true, true, fmt.Errorf("feed returned challenge-like HTML content")
	}
	format, rootName, err := detectFeedFormat(body)
	if err != nil {
		return nil, response.StatusCode, false, false, err
	}
	switch format {
	case "rss":
		var doc rssDoc
		if err := xml.Unmarshal(body, &doc); err != nil {
			return nil, response.StatusCode, false, false, err
		}
		papers := parseRSS(doc, url)
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "info",
			Component: "feeds",
			Action:    "feed_parsed",
			Message:   fmt.Sprintf("Parsed %d paper(s) from %s", len(papers), url),
			Data:      map[string]any{"url": url, "attempt": attempt, "format": "rss", "root": rootName},
		})
		return papers, response.StatusCode, false, false, nil
	case "atom":
		var doc atomDoc
		if err := xml.Unmarshal(body, &doc); err != nil {
			return nil, response.StatusCode, false, false, err
		}
		papers := parseAtom(doc, url)
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "info",
			Component: "feeds",
			Action:    "feed_parsed",
			Message:   fmt.Sprintf("Parsed %d paper(s) from %s", len(papers), url),
			Data:      map[string]any{"url": url, "attempt": attempt, "format": "atom", "root": rootName},
		})
		return papers, response.StatusCode, false, false, nil
	default:
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "warning",
			Component: "feeds",
			Action:    "feed_unknown_root",
			Message:   fmt.Sprintf("Feed returned unsupported XML root %q", rootName),
			Data:      map[string]any{"url": url, "attempt": attempt, "root": rootName},
		})
		return []store.Paper{}, response.StatusCode, false, false, nil
	}
}

func applyFeedRequestHeaders(request *http.Request) {
	request.Header.Set("User-Agent", "SciRSSAgent/0.1")
	request.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.7")
	request.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if referer := requestReferer(request.URL); referer != "" {
		request.Header.Set("Referer", referer)
	}
}

func requestReferer(target *neturl.URL) string {
	if target == nil || strings.TrimSpace(target.Scheme) == "" || strings.TrimSpace(target.Host) == "" {
		return ""
	}
	return target.Scheme + "://" + target.Host + "/"
}

func isRetryableFeedStatus(statusCode int) bool {
	return statusCode == http.StatusForbidden || statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

func looksLikeChallengeResponse(body []byte) bool {
	sample := strings.TrimSpace(string(body))
	if sample == "" {
		return false
	}
	if feedXMLPrefixRE.MatchString(sample) {
		return false
	}
	if feedHTMLPrefixRE.MatchString(sample) {
		return true
	}
	return feedChallengeMarkerRE.MatchString(sample)
}

func detectFeedFormat(body []byte) (string, string, error) {
	var probe feedRootProbe
	if err := xml.Unmarshal(body, &probe); err != nil {
		return "", "", err
	}
	rootName := strings.ToLower(probe.XMLName.Local)
	switch rootName {
	case "rss", "rdf":
		return "rss", rootName, nil
	case "feed":
		return "atom", rootName, nil
	}
	if probe.Channel != nil || len(probe.Items) > 0 {
		return "rss", rootName, nil
	}
	return "", rootName, nil
}

func parseRSS(doc rssDoc, sourceURL string) []store.Paper {
	items := doc.Channel.Items
	if len(items) == 0 {
		items = doc.Items
	}
	papers := make([]store.Paper, 0, len(items))
	for _, item := range items {
		title := normalizeText(item.Title)
		link := firstNonEmpty(normalizeText(item.Link), normalizeText(item.GUID))
		if title == "" || link == "" {
			continue
		}
		abstract, abstractHTML, images := chooseBestAbstract([]string{
			childTagInnerXML(item.InnerXML, "encoded"),
			childTagInnerXML(item.InnerXML, "description"),
			childTagText(item.InnerXML, "description"),
			childTagText(item.InnerXML, "description"),
		})
		authors := collectRSSAuthors(item)
		journal := normalizeFeedJournalTitle(firstNonEmpty(childTagText(item.InnerXML, "publicationName"), childTagText(item.InnerXML, "source"), doc.Channel.Title))
		doi := entryDOI(childTagText(item.InnerXML, "doi"), childTagText(item.InnerXML, "identifier"), item.GUID, link)
		raw := map[string]any{}
		if guid := normalizeText(item.GUID); guid != "" {
			raw["guid"] = guid
		}
		papers = append(papers, store.Paper{
			SourceURL:      sourceURL,
			FeedTitle:      stringPtr(normalizeText(doc.Channel.Title)),
			Title:          title,
			URL:            link,
			DOI:            stringPtr(doi),
			Journal:        stringPtr(journal),
			Authors:        authors,
			Abstract:       stringPtr(abstract),
			AbstractHTML:   stringPtr(abstractHTML),
			AbstractImages: images,
			AbstractSource: abstractSourceForContent(abstract, abstractHTML, images),
			PublishedDate:  parseEntryDateText(item.PubDate, childTagText(item.InnerXML, "date")),
			Raw:            raw,
		})
	}
	return papers
}

func parseAtom(doc atomDoc, sourceURL string) []store.Paper {
	papers := make([]store.Paper, 0, len(doc.Entries))
	for _, entry := range doc.Entries {
		title := normalizeText(entry.Title)
		link := ""
		for _, candidate := range entry.Links {
			if href := normalizeText(candidate.Href); href != "" {
				link = href
				break
			}
		}
		link = firstNonEmpty(link, normalizeText(entry.ID))
		if title == "" || link == "" {
			continue
		}
		abstract, abstractHTML, images := chooseBestAbstract([]string{
			entry.Content,
			entry.Summary,
			entry.DCDescription,
			entry.SummaryPlain,
		})
		authors := make([]string, 0, len(entry.Authors))
		for _, author := range entry.Authors {
			if name := normalizeText(author.Name); name != "" {
				authors = append(authors, name)
			}
		}
		journal := normalizeFeedJournalTitle(firstNonEmpty(entry.PrismPubName, entry.DCSource, doc.Title))
		doi := entryDOI(entry.ID, link)
		raw := map[string]any{}
		if id := normalizeText(entry.ID); id != "" {
			raw["id"] = id
		}
		papers = append(papers, store.Paper{
			SourceURL:      sourceURL,
			FeedTitle:      stringPtr(normalizeText(doc.Title)),
			Title:          title,
			URL:            link,
			DOI:            stringPtr(doi),
			Journal:        stringPtr(journal),
			Authors:        authors,
			Abstract:       stringPtr(abstract),
			AbstractHTML:   stringPtr(abstractHTML),
			AbstractImages: images,
			AbstractSource: abstractSourceForContent(abstract, abstractHTML, images),
			PublishedDate:  parseEntryDateText(entry.Published, entry.Updated),
			Raw:            raw,
		})
	}
	return papers
}

func chooseBestAbstract(candidates []string) (string, string, []store.AbstractImage) {
	bestText := ""
	bestHTML := ""
	bestImages := []store.AbstractImage{}
	for _, candidate := range candidates {
		text, htmlValue, images := normalizeAbstractCandidate(candidate)
		if text == "" && len(images) == 0 && strings.TrimSpace(htmlValue) == "" {
			continue
		}
		if len(text) > len(bestText) || (len(text) == len(bestText) && len(images) > len(bestImages)) {
			bestText = text
			bestHTML = htmlValue
			bestImages = images
		}
	}
	return bestText, bestHTML, bestImages
}

func normalizeAbstractCandidate(value string) (string, string, []store.AbstractImage) {
	rawHTML := cleanCDATA(strings.TrimSpace(html.UnescapeString(value)))
	images := extractImages(rawHTML)
	plain := normalizeText(stripTags(rawHTML))
	if plain == "" {
		if len(images) > 0 {
			return "", rawHTML, images
		}
		return "", "", nil
	}
	plain = stripKnownPrefixes(stripAbstractHeading(plain))
	if looksLikeMetadata(plain) {
		if len(images) > 0 {
			return "", rawHTML, images
		}
		return "", "", nil
	}
	if rawHTML == "" {
		return plain, "", images
	}
	return plain, rawHTML, images
}

func stripTags(value string) string {
	return tagRE.ReplaceAllString(value, " ")
}

func extractImages(value string) []store.AbstractImage {
	matches := imgSrcRE.FindAllStringSubmatch(value, -1)
	images := make([]store.AbstractImage, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		src := normalizeText(match[1])
		if src == "" {
			continue
		}
		if _, ok := seen[src]; ok {
			continue
		}
		seen[src] = struct{}{}
		images = append(images, store.AbstractImage{Src: src})
	}
	return images
}

func parseEntryDateText(values ...string) *string {
	for _, value := range values {
		clean := normalizeText(value)
		if clean == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC1123Z, clean); err == nil {
			formatted := parsed.UTC().Format("2006-01-02")
			return &formatted
		}
		if parsed, err := time.Parse(time.RFC1123, clean); err == nil {
			formatted := parsed.UTC().Format("2006-01-02")
			return &formatted
		}
		if parsed, err := time.Parse(time.RFC3339, clean); err == nil {
			formatted := parsed.UTC().Format("2006-01-02")
			return &formatted
		}
		if parsed, err := time.Parse("2006-01-02", clean); err == nil {
			formatted := parsed.Format("2006-01-02")
			return &formatted
		}
	}
	return nil
}

func entryDOI(values ...string) string {
	for _, value := range values {
		clean := normalizeText(value)
		if !strings.Contains(strings.ToLower(clean), "10.") {
			continue
		}
		match := doiValueRE.FindString(strings.ToUpper(clean))
		if match != "" {
			return strings.TrimSuffix(strings.ToLower(match), ".")
		}
		lowered := strings.ToLower(clean)
		start := strings.Index(lowered, "10.")
		if start >= 0 {
			doi := clean[start:]
			for _, sep := range []string{"?", "#", "&"} {
				if index := strings.Index(doi, sep); index >= 0 {
					doi = doi[:index]
				}
			}
			return strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(strings.ToLower(doi)), "doi:"), ".")
		}
	}
	return ""
}

func normalizeText(value string) string {
	return whitespaceRE.ReplaceAllString(strings.TrimSpace(html.UnescapeString(value)), " ")
}

func stripAbstractHeading(value string) string {
	indexes := abstractHeadingRE.FindStringIndex(value)
	if len(indexes) != 2 {
		return value
	}
	tail := strings.TrimSpace(value[indexes[1]:])
	if tail == "" {
		return value
	}
	return tail
}

func stripKnownPrefixes(value string) string {
	stripped := strings.TrimSpace(naturePrefixRE.ReplaceAllString(value, ""))
	if stripped == "" {
		return value
	}
	return stripped
}

func looksLikeMetadata(value string) bool {
	lowered := strings.ToLower(value)
	if len(value) < 120 && metadataRE.MatchString(lowered) {
		return true
	}
	if len(value) < 80 && strings.Count(value, ";")+strings.Count(value, ",") >= 3 && !strings.HasSuffix(value, ".") {
		return true
	}
	return false
}

func normalizeFeedJournalTitle(value string) string {
	clean := normalizeText(strings.SplitN(value, ", Published online:", 2)[0])
	if clean == "" {
		return ""
	}
	replacements := []struct{ old, new string }{
		{"Wiley: ", ""},
		{"AAAS: ", ""},
		{": Table of Contents", ""},
		{": Latest Articles (ACS Publications)", ""},
		{" (ACS Publications)", ""},
	}
	for _, replacement := range replacements {
		clean = strings.ReplaceAll(clean, replacement.old, replacement.new)
	}
	if parts := strings.Split(clean, ":"); len(parts) >= 2 && strings.TrimSpace(parts[0]) == "Science" {
		return "Science"
	}
	return strings.TrimSpace(clean)
}

func abstractSourceForContent(text string, htmlValue string, images []store.AbstractImage) string {
	if strings.TrimSpace(text) != "" || strings.TrimSpace(htmlValue) != "" || len(images) > 0 {
		return "rss"
	}
	return "none"
}

func childTagText(rawXML string, localName string) string {
	values := childTagTexts(rawXML, localName)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func childTagTexts(rawXML string, localName string) []string {
	re := regexp.MustCompile(fmt.Sprintf(`(?is)<(?:[\w-]+:)?%s\b[^>]*>(.*?)</(?:[\w-]+:)?%s>`, regexp.QuoteMeta(localName), regexp.QuoteMeta(localName)))
	matches := re.FindAllStringSubmatch(rawXML, -1)
	values := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		value := normalizeText(stripTags(cleanCDATA(match[1])))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func childTagInnerXML(rawXML string, localName string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?is)<(?:[\w-]+:)?%s\b[^>]*>(.*?)</(?:[\w-]+:)?%s>`, regexp.QuoteMeta(localName), regexp.QuoteMeta(localName)))
	match := re.FindStringSubmatch(rawXML)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func collectRSSAuthors(item rssItem) []string {
	authors := make([]string, 0, 1)
	for _, creator := range childTagTexts(item.InnerXML, "creator") {
		authors = appendUniqueAuthors(authors, creator)
	}
	if author := normalizeText(item.Author); author != "" {
		authors = appendUniqueAuthors(authors, author)
	}
	return authors
}

func appendUniqueAuthors(authors []string, raw string) []string {
	for _, author := range splitAuthorList(raw) {
		if !containsString(authors, author) {
			authors = append(authors, author)
		}
	}
	return authors
}

func splitAuthorList(raw string) []string {
	clean := normalizeText(raw)
	if clean == "" {
		return nil
	}
	if segments, ok := splitCommaPairAuthors(clean); ok {
		return segments
	}
	if segments, ok := splitDelimitedAuthors(clean, ";"); ok {
		return segments
	}
	if segments, ok := splitDelimitedAuthors(clean, " and "); ok {
		return segments
	}
	if segments, ok := splitCommaSeparatedAuthors(clean); ok {
		return segments
	}
	return []string{clean}
}

func splitCommaPairAuthors(value string) ([]string, bool) {
	parts := strings.Split(value, ",")
	if len(parts) < 4 || len(parts)%2 != 0 {
		return nil, false
	}
	authors := make([]string, 0, len(parts)/2)
	for i := 0; i < len(parts); i += 2 {
		family := normalizeText(parts[i])
		given := normalizeText(parts[i+1])
		if family == "" || given == "" || !looksLikeFamilyName(family) || !looksLikeGivenName(given) {
			return nil, false
		}
		authors = append(authors, family+", "+given)
	}
	return authors, len(authors) > 1
}

func splitDelimitedAuthors(value string, delimiter string) ([]string, bool) {
	if !strings.Contains(value, delimiter) {
		return nil, false
	}
	parts := strings.Split(value, delimiter)
	authors := make([]string, 0, len(parts))
	for _, part := range parts {
		name := normalizeText(part)
		if name == "" || !looksLikeAuthorName(name) {
			return nil, false
		}
		authors = append(authors, name)
	}
	return authors, len(authors) > 1
}

func splitCommaSeparatedAuthors(value string) ([]string, bool) {
	if strings.Count(value, ",") == 0 {
		return nil, false
	}
	parts := strings.Split(value, ",")
	if len(parts) < 2 {
		return nil, false
	}
	authors := make([]string, 0, len(parts))
	for _, part := range parts {
		name := normalizeText(part)
		if name == "" || !looksLikeAuthorName(name) {
			return nil, false
		}
		authors = append(authors, name)
	}
	return authors, len(authors) > 1
}

func looksLikeAuthorName(value string) bool {
	fields := strings.Fields(value)
	if len(fields) < 2 || len(fields) > 8 {
		return false
	}
	hasLetter := false
	for _, field := range fields {
		token := strings.Trim(field, ".,")
		if token == "" {
			continue
		}
		if strings.IndexFunc(token, func(r rune) bool { return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') }) >= 0 {
			hasLetter = true
		}
	}
	return hasLetter
}

func looksLikeFamilyName(value string) bool {
	fields := strings.Fields(value)
	if len(fields) == 0 || len(fields) > 5 {
		return false
	}
	for _, field := range fields {
		token := strings.Trim(field, ".,")
		if token == "" {
			return false
		}
	}
	return true
}

func looksLikeGivenName(value string) bool {
	fields := strings.Fields(value)
	if len(fields) == 0 || len(fields) > 4 {
		return false
	}
	hasLetter := false
	for _, field := range fields {
		token := strings.Trim(field, ".,")
		if token == "" {
			return false
		}
		if strings.IndexFunc(token, func(r rune) bool { return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') }) >= 0 {
			hasLetter = true
		}
	}
	return hasLetter
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringPtr(value string) *string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return nil
	}
	return &clean
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func ioReadAll(response *http.Response) ([]byte, error) {
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}

func cleanCDATA(value string) string {
	clean := strings.TrimSpace(value)
	clean = strings.TrimPrefix(clean, "<![CDATA[")
	clean = strings.TrimSuffix(clean, "]]>")
	return strings.TrimSpace(clean)
}
