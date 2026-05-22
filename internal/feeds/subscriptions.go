package feeds

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Subscription struct {
	Journal string `json:"journal"`
	URL     string `json:"url"`
}

type SettingsUpdateRequest struct {
	Feeds []Subscription `json:"feeds"`
}

func ReadSubscriptions(path string) ([]Subscription, error) {
	// 从 rss_feeds.json 读取订阅列表；没有文件时返回空列表。
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Subscription{}, nil
		}
		return nil, err
	}
	var feeds []Subscription
	if err := json.Unmarshal(data, &feeds); err != nil {
		return nil, err
	}
	for i := range feeds {
		normalized, err := NormalizeSubscription(feeds[i])
		if err != nil {
			return nil, err
		}
		feeds[i] = normalized
	}
	return feeds, nil
}

func WriteSubscriptions(path string, feeds []Subscription) ([]Subscription, error) {
	// 校验、标准化并去重后，把订阅列表写回磁盘。
	normalized := make([]Subscription, 0, len(feeds))
	seen := map[string]struct{}{}
	for _, feed := range feeds {
		item, err := NormalizeSubscription(feed)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[item.URL]; ok {
			continue
		}
		seen[item.URL] = struct{}{}
		normalized = append(normalized, item)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	payload, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return nil, err
	}
	return normalized, nil
}

func NormalizeSubscription(feed Subscription) (Subscription, error) {
	// 对单条订阅做最小清洗：journal 不能为空，URL 必须是 http/https。
	journal := strings.TrimSpace(feed.Journal)
	if journal == "" {
		return Subscription{}, errors.New("journal cannot be blank")
	}
	feedURL := strings.TrimSpace(feed.URL)
	parsed, err := url.ParseRequestURI(feedURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return Subscription{}, fmt.Errorf("invalid feed url: %s", feed.URL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Subscription{}, fmt.Errorf("feed url must start with http:// or https://")
	}
	if strings.EqualFold(parsed.Host, "www.cell.com") && parsed.Path == "/cell/current.rss" {
		parsed.Scheme = "https"
		feedURL = parsed.String()
	}
	return Subscription{Journal: journal, URL: feedURL}, nil
}
