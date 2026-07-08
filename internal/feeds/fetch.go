package feeds

import (
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"net/http"
	neturl "net/url"
	"sort"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/logging"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

type FetchProgressFunc func(current int, total int, label string)
type VerifyHostFunc func(requests []VerificationRequest) VerificationResult

type FetchOptions struct {
	MaxPapers        int
	SelectedFeedURLs []string
	OverrideBodies   map[string][]byte
	BodyCache        map[string][]byte
	SkippedFeeds     map[string]string
	Progress         FetchProgressFunc
	VerifyHost       VerifyHostFunc
}

type FetchResult struct {
	Papers               []store.Paper
	Errors               []string
	Fetched              int
	FeedURLs             []string
	VerificationRequests []VerificationRequest
}

type FeedError struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

type VerificationRequest struct {
	URL     string `json:"url"`
	Target  string `json:"target"`
	Reason  string `json:"reason"`
	Journal string `json:"journal,omitempty"`
}

type VerificationResult struct {
	FeedBodies map[string][]byte
	Warning    string
}

type FeedVerificationRequiredError struct {
	URL    string
	Target string
	Reason string
}

func (e *FeedVerificationRequiredError) Error() string {
	return fmt.Sprintf("%s requires manual verification", e.URL)
}

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

func parseFeedBody(sourceURL string, attempt int, body []byte) ([]store.Paper, error) {
	body = normalizeFeedBody(body)
	format, rootName, err := detectFeedFormat(body)
	if err != nil {
		return nil, err
	}
	switch format {
	case "rss":
		var doc rssDoc
		if err := xml.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		papers := parseRSS(doc, sourceURL)
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "info",
			Component: "feeds",
			Action:    "feed_parsed",
			Message:   fmt.Sprintf("Parsed %d paper(s) from %s", len(papers), sourceURL),
			Data:      map[string]any{"url": sourceURL, "attempt": attempt, "format": "rss", "root": rootName},
		})
		return papers, nil
	case "atom":
		var doc atomDoc
		if err := xml.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		papers := parseAtom(doc, sourceURL)
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "info",
			Component: "feeds",
			Action:    "feed_parsed",
			Message:   fmt.Sprintf("Parsed %d paper(s) from %s", len(papers), sourceURL),
			Data:      map[string]any{"url": sourceURL, "attempt": attempt, "format": "atom", "root": rootName},
		})
		return papers, nil
	default:
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "warning",
			Component: "feeds",
			Action:    "feed_unknown_root",
			Message:   fmt.Sprintf("Feed returned unsupported XML root %q", rootName),
			Data:      map[string]any{"url": sourceURL, "attempt": attempt, "root": rootName},
		})
		return []store.Paper{}, nil
	}
}

func normalizeFeedBody(body []byte) []byte {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return body
	}
	if snippet := extractEmbeddedFeedXML(trimmed); snippet != "" {
		return []byte(snippet)
	}
	return body
}

func extractEmbeddedFeedXML(raw string) string {
	candidates := []string{raw, html.UnescapeString(raw)}
	prefixes := []string{"<?xml", "<rss", "<feed", "<rdf:rdf"}
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		start := -1
		for _, prefix := range prefixes {
			idx := strings.Index(lower, prefix)
			if idx >= 0 && (start == -1 || idx < start) {
				start = idx
			}
		}
		if start < 0 {
			continue
		}
		snippet := strings.TrimSpace(trimmed[start:])
		if len(snippet) < 32 {
			continue
		}
		snippetLower := strings.ToLower(snippet)
		if strings.Contains(snippetLower, "<item") ||
			strings.Contains(snippetLower, "<entry") ||
			strings.Contains(snippetLower, "<channel") ||
			strings.Contains(snippetLower, "<rdf:rdf") {
			return snippet
		}
	}
	return ""
}

func feedOverrideBody(url string, overrides map[string][]byte) ([]byte, bool) {
	if len(overrides) == 0 {
		return nil, false
	}
	body, ok := overrides[url]
	if !ok || len(body) == 0 {
		return nil, false
	}
	return body, true
}

func feedCachedBody(url string, cache map[string][]byte) ([]byte, bool) {
	if len(cache) == 0 {
		return nil, false
	}
	body, ok := cache[url]
	if !ok || len(body) == 0 {
		return nil, false
	}
	return body, true
}

func rememberFeedBody(url string, cache map[string][]byte, body []byte) {
	if cache == nil || len(body) == 0 {
		return
	}
	cache[url] = append([]byte(nil), body...)
}

func skippedFeedReason(url string, skipped map[string]string) (string, bool) {
	if len(skipped) == 0 {
		return "", false
	}
	reason, ok := skipped[url]
	if !ok || strings.TrimSpace(reason) == "" {
		return "", false
	}
	return strings.TrimSpace(reason), true
}

func sortSubscriptionsByHost(subscriptions []Subscription) []Subscription {
	sorted := append([]Subscription(nil), subscriptions...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left := feedHostKey(sorted[i].URL)
		right := feedHostKey(sorted[j].URL)
		if left == right {
			return false
		}
		return left < right
	})
	return sorted
}

func feedHostKey(rawURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parsed.Hostname()))
}

func filterSubscriptionsByURLs(subscriptions []Subscription, selectedURLs []string) []Subscription {
	if len(selectedURLs) == 0 {
		return subscriptions
	}
	selected := map[string]struct{}{}
	for _, rawURL := range selectedURLs {
		feedURL := strings.TrimSpace(rawURL)
		if feedURL != "" {
			selected[feedURL] = struct{}{}
		}
	}
	if len(selected) == 0 {
		return subscriptions
	}
	filtered := make([]Subscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if _, ok := selected[strings.TrimSpace(subscription.URL)]; ok {
			filtered = append(filtered, subscription)
		}
	}
	return filtered
}

// FetchAll reads configured feeds, fetches them, and normalizes entries into paper candidates.
func FetchAll(feedsPath string, opts FetchOptions) (FetchResult, error) {
	subscriptions, err := ReadSubscriptions(feedsPath)
	if err != nil {
		return FetchResult{}, err
	}
	subscriptions = filterSubscriptionsByURLs(subscriptions, opts.SelectedFeedURLs)
	subscriptions = sortSubscriptionsByHost(subscriptions)
	totalFeeds := len(subscriptions)
	if opts.Progress != nil {
		opts.Progress(0, totalFeeds, "")
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "feeds",
		Action:    "fetch_all_started",
		Message:   fmt.Sprintf("Fetching %d feeds", len(subscriptions)),
	})
	result := FetchResult{
		Papers:               []store.Paper{},
		Errors:               []string{},
		Fetched:              0,
		FeedURLs:             make([]string, 0, len(subscriptions)),
		VerificationRequests: []VerificationRequest{},
	}
	for index := 0; index < len(subscriptions); index++ {
		subscription := subscriptions[index]
		result.FeedURLs = append(result.FeedURLs, subscription.URL)
		label := fetchProgressLabel(subscription.Journal, subscription.URL)
		if opts.Progress != nil {
			opts.Progress(index+1, totalFeeds, label)
		}
		if reason, ok := skippedFeedReason(subscription.URL, opts.SkippedFeeds); ok {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", subscription.URL, reason))
			continue
		}
		fetched, err := fetchFeed(subscription.URL, opts)
		if err != nil {
			var verificationErr *FeedVerificationRequiredError
			if errors.As(err, &verificationErr) {
				requests, nextIndex := hostVerificationRequests(subscriptions, index, verificationErr, opts)
				if opts.VerifyHost != nil {
					result.FeedURLs = appendResultFeedURLs(result.FeedURLs, subscriptions[index+1:nextIndex])
					for progressIndex := index + 1; progressIndex < nextIndex; progressIndex++ {
						if opts.Progress != nil {
							opts.Progress(progressIndex+1, totalFeeds, fetchProgressLabel(subscriptions[progressIndex].Journal, subscriptions[progressIndex].URL))
						}
					}
					result.Papers = append(result.Papers, resolveHostVerification(requests, opts, &result)...)
					index = nextIndex - 1
					continue
				}
				result.VerificationRequests = append(result.VerificationRequests, VerificationRequest{
					URL:     subscription.URL,
					Target:  verificationErr.Target,
					Reason:  verificationErr.Reason,
					Journal: subscription.Journal,
				})
				continue
			}
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", subscription.URL, err))
			continue
		}
		rememberFeedBody(subscription.URL, opts.BodyCache, fetched.Body)
		result.Papers = append(result.Papers, fetched.Papers...)
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

func appendResultFeedURLs(existing []string, subscriptions []Subscription) []string {
	for _, subscription := range subscriptions {
		existing = append(existing, subscription.URL)
	}
	return existing
}

func hostVerificationRequests(subscriptions []Subscription, start int, verificationErr *FeedVerificationRequiredError, opts FetchOptions) ([]VerificationRequest, int) {
	if start >= len(subscriptions) {
		return nil, start
	}
	host := feedHostKey(subscriptions[start].URL)
	end := start
	for end < len(subscriptions) && feedHostKey(subscriptions[end].URL) == host {
		end++
	}
	requests := make([]VerificationRequest, 0, end-start)
	for _, subscription := range subscriptions[start:end] {
		if _, ok := skippedFeedReason(subscription.URL, opts.SkippedFeeds); ok {
			continue
		}
		if _, ok := feedOverrideBody(subscription.URL, opts.OverrideBodies); ok {
			continue
		}
		if _, ok := feedCachedBody(subscription.URL, opts.BodyCache); ok {
			continue
		}
		requests = append(requests, VerificationRequest{
			URL:     subscription.URL,
			Target:  verificationErr.Target,
			Reason:  verificationErr.Reason,
			Journal: subscription.Journal,
		})
	}
	if len(requests) == 0 {
		requests = append(requests, VerificationRequest{
			URL:     subscriptions[start].URL,
			Target:  verificationErr.Target,
			Reason:  verificationErr.Reason,
			Journal: subscriptions[start].Journal,
		})
	}
	return requests, end
}

func resolveHostVerification(requests []VerificationRequest, opts FetchOptions, result *FetchResult) []store.Paper {
	verification := opts.VerifyHost(requests)
	if strings.TrimSpace(verification.Warning) != "" {
		for _, request := range requests {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", request.URL, strings.TrimSpace(verification.Warning)))
		}
		return nil
	}
	papers := []store.Paper{}
	for _, request := range requests {
		body, ok := verification.FeedBodies[request.URL]
		if !ok || len(body) == 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: verification did not return feed XML", request.URL))
			continue
		}
		parsed, err := parseFeedBody(request.URL, 0, body)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", request.URL, err))
			continue
		}
		rememberFeedBody(request.URL, opts.BodyCache, body)
		papers = append(papers, parsed...)
	}
	return papers
}

func fetchProgressLabel(journal string, rawURL string) string {
	if strings.TrimSpace(journal) != "" {
		return strings.TrimSpace(journal)
	}
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err == nil && strings.TrimSpace(parsed.Hostname()) != "" {
		return strings.TrimSpace(parsed.Hostname())
	}
	return strings.TrimSpace(rawURL)
}

type fetchedFeed struct {
	Papers []store.Paper
	Body   []byte
}

func fetchFeed(url string, opts FetchOptions) (fetchedFeed, error) {
	if body, ok := feedOverrideBody(url, opts.OverrideBodies); ok {
		papers, err := parseFeedBody(url, 0, body)
		return fetchedFeed{Papers: papers, Body: body}, err
	}
	if body, ok := feedCachedBody(url, opts.BodyCache); ok {
		papers, err := parseFeedBody(url, 0, body)
		return fetchedFeed{Papers: papers, Body: body}, err
	}
	attemptCount := len(fetchRetryBackoffs) + 1
	attempted := 0
	lastStatusCode := 0
	lastChallenge := false
	var lastErr error
	for attempt := 1; attempt <= attemptCount; attempt++ {
		attempted = attempt
		fetched, statusCode, challenge, retryable, err := fetchFeedAttempt(url, attempt)
		if err == nil {
			return fetched, nil
		}
		lastStatusCode = statusCode
		lastChallenge = challenge
		lastErr = err
		var verificationErr *FeedVerificationRequiredError
		if errors.As(err, &verificationErr) {
			break
		}
		if !retryable || attempt == attemptCount {
			break
		}
		time.Sleep(fetchRetryBackoffs[attempt-1])
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "warning",
		Component: "feeds",
		Action:    "feed_fetch_failed",
		Message:   fmt.Sprintf("Failed to fetch feed after %d attempt(s): %s", attempted, url),
		Error:     lastErr.Error(),
		Data: map[string]any{
			"url":                 url,
			"attempts":            attempted,
			"last_status_code":    lastStatusCode,
			"challenge_suspected": lastChallenge,
		},
	})
	return fetchedFeed{}, lastErr
}

func fetchFeedAttempt(url string, attempt int) (fetchedFeed, int, bool, bool, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fetchedFeed{}, 0, false, false, err
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
		return fetchedFeed{}, 0, false, true, err
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
	body, err := ioReadAll(response)
	if err != nil {
		return fetchedFeed{}, response.StatusCode, false, true, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryable := isRetryableFeedStatus(response.StatusCode)
		if shouldRequireFeedVerification(url, response, false) {
			return fetchedFeed{}, response.StatusCode, false, retryable, &FeedVerificationRequiredError{
				URL:    url,
				Target: verificationTargetForURL(url),
				Reason: "challenge",
			}
		}
		return fetchedFeed{}, response.StatusCode, false, retryable, fmt.Errorf("request failed with %s", response.Status)
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
		if shouldRequireFeedVerification(url, response, true) {
			return fetchedFeed{}, response.StatusCode, true, true, &FeedVerificationRequiredError{
				URL:    url,
				Target: verificationTargetForURL(url),
				Reason: "challenge",
			}
		}
		return fetchedFeed{}, response.StatusCode, true, true, fmt.Errorf("feed returned challenge-like HTML content")
	}
	papers, err := parseFeedBody(url, attempt, body)
	if err != nil {
		return fetchedFeed{}, response.StatusCode, false, false, err
	}
	return fetchedFeed{Papers: papers, Body: body}, response.StatusCode, false, false, nil
}
