package feeds

import (
	"fmt"
	"regexp"
	"strings"

	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
	"golang.org/x/net/html"
)

var (
	rscTOCSubject = regexp.MustCompile(`(?i)^(.+?) Table of Contents for .+: Volume [^,]+, Issue .+$`)
	rscDOI        = regexp.MustCompile(`(?i)10\.\d{4,9}/[-._;()/:a-z0-9]+`)
	rscTitleStyle = regexp.MustCompile(`(?i)font-size\s*:\s*22px`)
)

// KTN publishes one RSS item per email; RSC's issue alerts contain many papers.
func parseKTNRSC(doc rssDoc, sourceURL string) ([]store.Paper, error) {
	items := doc.Channel.Items
	if len(items) == 0 {
		items = doc.Items
	}
	papers := make([]store.Paper, 0)
	for _, item := range items {
		match := rscTOCSubject.FindStringSubmatch(normalizeText(item.Title))
		if match == nil {
			continue // Account, confirmation, and other non-issue emails are not papers.
		}
		journal := normalizeText(match[1])
		body := cleanCDATA(childTagInnerXML(item.InnerXML, "encoded"))
		if body == "" {
			return nil, fmt.Errorf("RSC issue alert %q has no email HTML", item.Title)
		}
		document, err := html.Parse(strings.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("parse RSC issue alert %q: %w", item.Title, err)
		}
		if !strings.Contains(strings.ToLower(rscVisibleText(document)), strings.ToLower(journal+" Latest issue Alert")) {
			return nil, fmt.Errorf("RSC issue alert %q does not match the email journal", item.Title)
		}
		entries, err := rscPapers(document, sourceURL, doc.Channel.Title, journal)
		if err != nil {
			return nil, fmt.Errorf("RSC issue alert %q: %w", item.Title, err)
		}
		if len(entries) == 0 {
			return nil, fmt.Errorf("RSC issue alert %q: no papers extracted", item.Title)
		}
		papers = append(papers, entries...)
	}
	return papers, nil
}

func rscPapers(document *html.Node, sourceURL, feedTitle, journal string) ([]store.Paper, error) {
	papers := make([]store.Paper, 0)
	title := ""
	var authors []string
	var visit func(*html.Node)
	var parseErr error
	visit = func(node *html.Node) {
		if parseErr != nil || node.Type == html.ElementNode && rscHidden(node) {
			return
		}
		if node.Type == html.ElementNode && node.Data == "td" && !rscHasNestedTD(node) {
			text := normalizeText(rscVisibleText(node))
			if doi := rscDOI.FindString(text); doi != "" {
				if title == "" {
					parseErr = fmt.Errorf("DOI %q has no preceding article title", doi)
					return
				}
				if !rscNonPaperTitle(title) {
					doi = strings.TrimRight(strings.ToLower(doi), ".,;:)]}")
					papers = append(papers, store.Paper{
						SourceURL: sourceURL,
						FeedTitle: stringPtr(normalizeText(feedTitle)),
						Title:     title,
						URL:       "https://doi.org/" + doi,
						DOI:       stringPtr(doi),
						Journal:   stringPtr(journal),
						Authors:   append([]string(nil), authors...),
						Raw:       map[string]any{"source": "ktn_rsc_toc"},
					})
				}
				title, authors = "", nil
				return
			}
			if candidate := rscTitleAnchor(node); candidate != "" {
				title, authors = candidate, nil
				return
			}
			if title != "" {
				if found := rscAuthorAnchors(node); len(found) > 0 {
					authors = found
				}
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	return papers, parseErr
}

func rscNonPaperTitle(title string) bool {
	switch strings.ToLower(normalizeText(title)) {
	case "front cover", "inside front cover", "back cover", "inside back cover", "contents list":
		return true
	}
	return false
}

func rscTitleAnchor(cell *html.Node) string {
	cellTitle := rscTitleStyle.MatchString(rscAttr(cell, "style"))
	for child := cell.FirstChild; child != nil; child = child.NextSibling {
		if title := rscTitleAnchorNode(child, cellTitle); title != "" {
			return title
		}
	}
	return ""
}

func rscTitleAnchorNode(node *html.Node, cellTitle bool) string {
	if node.Type == html.ElementNode && node.Data == "a" && (cellTitle || rscTitleStyle.MatchString(rscAttr(node, "style"))) {
		return normalizeText(rscVisibleText(node))
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if title := rscTitleAnchorNode(child, cellTitle); title != "" {
			return title
		}
	}
	return ""
}

func rscAuthorAnchors(cell *html.Node) []string {
	var authors []string
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" && strings.Contains(strings.ToLower(rscAttr(node, "style")), "#006fb7") {
			if name := normalizeText(rscVisibleText(node)); name != "" {
				authors = append(authors, name)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(cell)
	return authors
}

func rscVisibleText(node *html.Node) string {
	var text strings.Builder
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.TextNode {
			text.WriteString(current.Data)
			text.WriteByte(' ')
			return
		}
		if current.Type == html.ElementNode && rscHidden(current) {
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return normalizeText(text.String())
}

func rscHidden(node *html.Node) bool {
	switch node.Data {
	case "head", "style", "script", "noscript", "iframe":
		return true
	}
	style := strings.ReplaceAll(strings.ToLower(rscAttr(node, "style")), " ", "")
	return strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden") || rscAttr(node, "hidden") != ""
}

func rscHasNestedTD(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "td" || rscHasNestedTD(child) {
			return true
		}
	}
	return false
}

func rscAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}
