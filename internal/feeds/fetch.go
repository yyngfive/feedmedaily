package feeds

import (
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

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
	fetchHTTPClient   = &http.Client{Timeout: 30 * time.Second}
	whitespaceRE      = regexp.MustCompile(`\s+`)
	tagRE             = regexp.MustCompile(`<[^>]+>`)
	imgSrcRE          = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)
	metadataRE        = regexp.MustCompile(`(?i)\b(vol(?:ume)?|issue|pp?\.|pages?|doi|e?issn|published|online)\b`)
	abstractHeadingRE = regexp.MustCompile(`(?i)(?:^|\s)ABSTRACT[:\s-]*`)
	naturePrefixRE    = regexp.MustCompile(`(?i)^[^.]*?,\s*Published online:\s*.*?;\s*doi:\S+\s*`)
	doiValueRE        = regexp.MustCompile(`10\.\d{4,9}/[-._;()/:A-Z0-9]+`)
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

// FetchAll reads configured feeds, fetches them, and normalizes entries into paper candidates.
func FetchAll(feedsPath string, opts FetchOptions) (FetchResult, error) {
	subscriptions, err := ReadSubscriptions(feedsPath)
	if err != nil {
		return FetchResult{}, err
	}
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
		result.Papers = result.Papers[:opts.MaxPapers]
	}
	result.Fetched = len(result.Papers)
	return result, nil
}

func fetchFeed(url string) ([]store.Paper, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "SciRSSAgent/0.1")
	response, err := fetchHTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		return nil, fmt.Errorf("request failed with %s", response.Status)
	}
	var root struct {
		XMLName xml.Name
	}
	body, err := ioReadAll(response)
	if err != nil {
		return nil, err
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	switch strings.ToLower(root.XMLName.Local) {
	case "rss":
		var doc rssDoc
		if err := xml.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		return parseRSS(doc, url), nil
	case "feed":
		var doc atomDoc
		if err := xml.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		return parseAtom(doc, url), nil
	default:
		return []store.Paper{}, nil
	}
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
		authors := []string{}
		if creator := normalizeText(childTagText(item.InnerXML, "creator")); creator != "" {
			authors = append(authors, creator)
		}
		if author := normalizeText(item.Author); author != "" && !containsString(authors, author) {
			authors = append(authors, author)
		}
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
		if text == "" {
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
	plain := normalizeText(stripTags(rawHTML))
	if plain == "" {
		return "", "", nil
	}
	plain = stripKnownPrefixes(stripAbstractHeading(plain))
	if looksLikeMetadata(plain) {
		return "", "", nil
	}
	images := extractImages(rawHTML)
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
	re := regexp.MustCompile(fmt.Sprintf(`(?is)<(?:[\w-]+:)?%s\b[^>]*>(.*?)</(?:[\w-]+:)?%s>`, regexp.QuoteMeta(localName), regexp.QuoteMeta(localName)))
	match := re.FindStringSubmatch(rawXML)
	if len(match) < 2 {
		return ""
	}
	return normalizeText(stripTags(match[1]))
}

func childTagInnerXML(rawXML string, localName string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?is)<(?:[\w-]+:)?%s\b[^>]*>(.*?)</(?:[\w-]+:)?%s>`, regexp.QuoteMeta(localName), regexp.QuoteMeta(localName)))
	match := re.FindStringSubmatch(rawXML)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
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
