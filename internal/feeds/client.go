package feeds

import (
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"
)

var (
	fetchHTTPClient       = &http.Client{Timeout: 30 * time.Second}
	fetchRetryBackoffs    = []time.Duration{200 * time.Millisecond, 600 * time.Millisecond}
	feedXMLPrefixRE       = regexp.MustCompile(`(?is)^\s*(?:<\?xml\b[^>]*>\s*)?<(?:rss|rdf:RDF|feed)\b`)
	feedHTMLPrefixRE      = regexp.MustCompile(`(?is)^\s*(?:<!doctype\s+html\b\s*>)?\s*<html\b`)
	feedChallengeMarkerRE = regexp.MustCompile(`(?is)(just a moment|enable javascript and cookies|attention required|__cf_chl_|cf-browser-verification|challenge-platform)`)
)

func applyFeedRequestHeaders(request *http.Request) {
	request.Header.Set("User-Agent", feedBrowserUserAgent())
	request.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.7")
	request.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if referer := requestReferer(request.URL); referer != "" {
		request.Header.Set("Referer", referer)
	}
}

func feedBrowserUserAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36 SciRSSAgent/0.1"
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

func shouldRequireFeedVerification(feedURL string, statusCode int, challenge bool) bool {
	target := verificationTargetForURL(feedURL)
	if target == "" {
		return false
	}
	if challenge {
		return true
	}
	return statusCode == http.StatusForbidden
}

func verificationTargetForURL(feedURL string) string {
	parsed, err := neturl.Parse(feedURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "chemrxiv.org" {
		return "chemrxiv"
	}
	return ""
}

func ioReadAll(response *http.Response) ([]byte, error) {
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}
