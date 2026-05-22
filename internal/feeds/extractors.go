package feeds

import (
	"html"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

var (
	whitespaceRE      = regexp.MustCompile(`\s+`)
	tagRE             = regexp.MustCompile(`<[^>]+>`)
	imgSrcRE          = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)
	metadataRE        = regexp.MustCompile(`(?i)\b(vol(?:ume)?|issue|pp?\.|pages?|doi|e?issn|published|online)\b`)
	abstractHeadingRE = regexp.MustCompile(`(?i)(?:^|\s)ABSTRACT[:\s-]*`)
	naturePrefixRE    = regexp.MustCompile(`(?i)^[^.]*?,\s*Published online:\s*.*?;\s*doi:\S+\s*`)
	doiValueRE        = regexp.MustCompile(`10\.\d{4,9}/[-._;()/:A-Z0-9]+`)
	elsevierDateRE    = regexp.MustCompile(`(?i)(\d{1,2}\s+[A-Za-z]+\s+\d{4})`)
)

type elsevierDescription struct {
	Authors       []string
	Journal       string
	PublishedDate *string
	AbstractText  string
	AbstractHTML  string
}

func extractRSSItemMetadata(feedTitle string, sourceURL string, link string, item rssItem, descriptionHTML string, descriptionText string) (string, string, []store.AbstractImage, []string, string, *string) {
	elsevierActive := isElsevierFeed(sourceURL, link)
	elsevierDetails := extractElsevierDescription(sourceURL, link, descriptionHTML, descriptionText)
	abstractCandidates := []string{childTagInnerXML(item.InnerXML, "encoded")}
	if elsevierDetails.AbstractHTML != "" || elsevierDetails.AbstractText != "" {
		abstractCandidates = append(abstractCandidates, elsevierDetails.AbstractHTML, elsevierDetails.AbstractText)
	} else if !elsevierActive {
		abstractCandidates = append(abstractCandidates, descriptionHTML, descriptionText, descriptionText)
	}
	abstract, abstractHTML, images := chooseBestAbstract(abstractCandidates)
	authors := collectRSSAuthors(item)
	if len(authors) == 0 && len(elsevierDetails.Authors) > 0 {
		authors = append([]string{}, elsevierDetails.Authors...)
	}
	journal := normalizeFeedJournalTitle(firstNonEmpty(childTagText(item.InnerXML, "publicationName"), childTagText(item.InnerXML, "source"), feedTitle))
	if isGenericElsevierJournalTitle(journal) && strings.TrimSpace(elsevierDetails.Journal) != "" {
		journal = normalizeFeedJournalTitle(elsevierDetails.Journal)
	} else if journal == "" && strings.TrimSpace(elsevierDetails.Journal) != "" {
		journal = normalizeFeedJournalTitle(elsevierDetails.Journal)
	}
	publishedDate := parseEntryDateText(item.PubDate, childTagText(item.InnerXML, "date"))
	if publishedDate == nil && elsevierDetails.PublishedDate != nil {
		publishedDate = elsevierDetails.PublishedDate
	}
	return abstract, abstractHTML, images, authors, journal, publishedDate
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
		family := cleanupAuthorSegment(parts[i])
		given := cleanupAuthorSegment(parts[i+1])
		if family == "" || given == "" || !looksLikeFamilyName(family) || !looksLikeLastFirstGivenName(given) {
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
		name := cleanupAuthorSegment(part)
		if name == "" {
			return nil, false
		}
		if nested, ok := splitCommaSeparatedAuthors(name); ok {
			authors = append(authors, nested...)
			continue
		}
		if !looksLikeAuthorName(name) {
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
		name := cleanupAuthorSegment(part)
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

func looksLikeLastFirstGivenName(value string) bool {
	if !looksLikeGivenName(value) {
		return false
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return false
	}
	if len(fields) == 1 {
		return true
	}
	for _, field := range fields {
		token := strings.Trim(field, ".,")
		if token == "" {
			return false
		}
		if len(token) <= 2 {
			continue
		}
		return false
	}
	return true
}

func cleanupAuthorSegment(value string) string {
	clean := normalizeText(value)
	clean = strings.TrimSpace(strings.TrimSuffix(clean, ","))
	if strings.HasPrefix(strings.ToLower(clean), "and ") {
		clean = strings.TrimSpace(clean[4:])
	}
	return strings.TrimSpace(clean)
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

func extractElsevierDescription(sourceURL string, articleURL string, descriptionHTML string, descriptionText string) elsevierDescription {
	if !isElsevierFeed(sourceURL, articleURL) {
		return elsevierDescription{}
	}
	blocks := extractElsevierDescriptionBlocks(descriptionHTML, descriptionText)
	result := elsevierDescription{}
	abstractBlocks := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch {
		case strings.HasPrefix(strings.ToLower(block), "publication date:"):
			if result.PublishedDate == nil {
				result.PublishedDate = parseElsevierPublicationDate(block)
			}
		case strings.HasPrefix(strings.ToLower(block), "source:"):
			result.Journal = strings.TrimSpace(strings.TrimPrefix(block, "Source:"))
			if result.Journal == block {
				result.Journal = strings.TrimSpace(strings.TrimPrefix(block, "source:"))
			}
		case strings.HasPrefix(strings.ToLower(block), "author(s):"):
			rawAuthors := strings.TrimSpace(strings.TrimPrefix(block, "Author(s):"))
			if rawAuthors == block {
				rawAuthors = strings.TrimSpace(strings.TrimPrefix(block, "author(s):"))
			}
			result.Authors = splitElsevierAuthors(rawAuthors)
		default:
			abstractBlocks = append(abstractBlocks, block)
		}
	}
	if len(abstractBlocks) > 0 {
		result.AbstractText = strings.Join(abstractBlocks, " ")
		result.AbstractHTML = "<p>" + html.EscapeString(strings.Join(abstractBlocks, "</p><p>")) + "</p>"
	}
	return result
}

func isElsevierFeed(sourceURL string, articleURL string) bool {
	for _, rawURL := range []string{sourceURL, articleURL} {
		target, err := neturl.Parse(rawURL)
		if err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(target.Host)) {
		case "rss.sciencedirect.com", "www.sciencedirect.com", "sciencedirect.com":
			return true
		}
	}
	return false
}

func extractElsevierDescriptionBlocks(descriptionHTML string, descriptionText string) []string {
	raw := strings.TrimSpace(descriptionHTML)
	if raw != "" {
		matches := regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`).FindAllStringSubmatch(raw, -1)
		blocks := make([]string, 0, len(matches))
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			text := normalizeText(stripTags(match[1]))
			if text != "" {
				blocks = append(blocks, text)
			}
		}
		if len(blocks) > 0 {
			return blocks
		}
	}
	if text := normalizeText(descriptionText); text != "" {
		return []string{text}
	}
	return nil
}

func parseElsevierPublicationDate(value string) *string {
	match := elsevierDateRE.FindStringSubmatch(value)
	if len(match) < 2 {
		return nil
	}
	if parsed, err := time.Parse("2 January 2006", match[1]); err == nil {
		formatted := parsed.Format("2006-01-02")
		return &formatted
	}
	return nil
}

func splitElsevierAuthors(value string) []string {
	clean := normalizeText(value)
	if clean == "" {
		return nil
	}
	parts := strings.Split(clean, ",")
	authors := make([]string, 0, len(parts))
	for _, part := range parts {
		name := normalizeText(strings.TrimSuffix(strings.TrimSpace(part), "."))
		if name == "" {
			continue
		}
		authors = append(authors, name)
	}
	return authors
}

func isGenericElsevierJournalTitle(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "sciencedirect publication:")
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func cleanCDATA(value string) string {
	clean := strings.TrimSpace(value)
	clean = strings.TrimPrefix(clean, "<![CDATA[")
	clean = strings.TrimSuffix(clean, "]]>")
	return strings.TrimSpace(clean)
}
