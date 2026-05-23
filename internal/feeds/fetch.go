package feeds

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/logging"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

type FetchOptions struct {
	MaxPapers      int
	OverrideBodies map[string][]byte
	SkippedFeeds   map[string]string
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
		Papers:               []store.Paper{},
		Errors:               []string{},
		Fetched:              0,
		FeedURLs:             make([]string, 0, len(subscriptions)),
		VerificationRequests: []VerificationRequest{},
	}
	for _, subscription := range subscriptions {
		result.FeedURLs = append(result.FeedURLs, subscription.URL)
		if reason, ok := skippedFeedReason(subscription.URL, opts.SkippedFeeds); ok {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", subscription.URL, reason))
			continue
		}
		papers, err := fetchFeed(subscription.URL, opts)
		if err != nil {
			var verificationErr *FeedVerificationRequiredError
			if errors.As(err, &verificationErr) {
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

func fetchFeed(url string, opts FetchOptions) ([]store.Paper, error) {
	if body, ok := feedOverrideBody(url, opts.OverrideBodies); ok {
		return parseFeedBody(url, 0, body)
	}
	attemptCount := len(fetchRetryBackoffs) + 1
	attempted := 0
	lastStatusCode := 0
	lastChallenge := false
	var lastErr error
	for attempt := 1; attempt <= attemptCount; attempt++ {
		attempted = attempt
		papers, statusCode, challenge, retryable, err := fetchFeedAttempt(url, attempt)
		if err == nil {
			return papers, nil
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
	body, err := ioReadAll(response)
	if err != nil {
		return nil, response.StatusCode, false, true, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryable := isRetryableFeedStatus(response.StatusCode)
		if shouldRequireFeedVerification(url, response, false) {
			return nil, response.StatusCode, false, retryable, &FeedVerificationRequiredError{
				URL:    url,
				Target: verificationTargetForURL(url),
				Reason: "challenge",
			}
		}
		return nil, response.StatusCode, false, retryable, fmt.Errorf("request failed with %s", response.Status)
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
			return nil, response.StatusCode, true, true, &FeedVerificationRequiredError{
				URL:    url,
				Target: verificationTargetForURL(url),
				Reason: "challenge",
			}
		}
		return nil, response.StatusCode, true, true, fmt.Errorf("feed returned challenge-like HTML content")
	}
	papers, err := parseFeedBody(url, attempt, body)
	if err != nil {
		return nil, response.StatusCode, false, false, err
	}
	return papers, response.StatusCode, false, false, nil
}
