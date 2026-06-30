package feeds

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"

	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

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

func ValidateFeedXML(sourceURL string, body []byte) ([]byte, error) {
	normalized := normalizeFeedBody(body)
	format, rootName, err := detectFeedFormat(normalized)
	if err != nil {
		return nil, err
	}
	if format == "" {
		if strings.TrimSpace(rootName) == "" {
			return nil, fmt.Errorf("Feed XML could not be parsed.")
		}
		return nil, fmt.Errorf("Feed XML must be RSS, Atom, or RDF. Got root %q.", rootName)
	}
	papers, err := parseFeedBody(sourceURL, 0, normalized)
	if err != nil {
		return nil, err
	}
	if len(papers) == 0 {
		return nil, fmt.Errorf("Feed XML did not contain any supported items or entries.")
	}
	return normalized, nil
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
		descriptionHTML := childTagInnerXML(item.InnerXML, "description")
		descriptionText := childTagText(item.InnerXML, "description")
		abstract, abstractHTML, images, authors, journal, publishedDate := extractRSSItemMetadata(doc.Channel.Title, sourceURL, link, item, descriptionHTML, descriptionText)
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
			PublishedDate:  publishedDate,
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
		}, link, sourceURL)
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

func childTagText(rawXML string, localName string) string {
	values := childTagTexts(rawXML, localName)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func childTagTexts(rawXML string, localName string) []string {
	re := regexpForLocalName(localName)
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
	match := regexpForLocalName(localName).FindStringSubmatch(rawXML)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func regexpForLocalName(localName string) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`(?is)<(?:[\w-]+:)?%s\b[^>]*>(.*?)</(?:[\w-]+:)?%s>`, regexp.QuoteMeta(localName), regexp.QuoteMeta(localName)))
}
