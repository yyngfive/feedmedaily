package zotero

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/logging"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

type CollectionOption struct {
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	PathLabel string  `json:"path_label"`
	ParentKey *string `json:"parent_key"`
	IsDefault bool    `json:"is_default"`
}

type CollectionsResponse struct {
	Collections          []CollectionOption `json:"collections"`
	DefaultCollectionKey *string            `json:"default_collection_key"`
}

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	apiBaseURL = "https://api.zotero.org"
)

func ListCollections(settings config.Settings) (CollectionsResponse, error) {
	// 读取 Zotero collections，并构造成前端现有的平铺树形响应。
	prefix, err := libraryPrefix(settings)
	if err != nil {
		return CollectionsResponse{}, err
	}
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(apiBaseURL, "/")+"/"+prefix+"/collections?format=json&limit=1000", nil)
	if err != nil {
		return CollectionsResponse{}, err
	}
	request.Header.Set("Zotero-API-Version", "3")
	request.Header.Set("Zotero-API-Key", settings.ZoteroAPIKey)
	started := time.Now()
	response, err := httpClient.Do(request)
	if err != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "error",
			Component: "zotero",
			Action:    "list_collections_failed",
			Message:   fmt.Sprintf("HTTP Request: GET %s failed", request.URL.String()),
			Error:     err.Error(),
			Data:      map[string]any{"duration_ms": time.Since(started).Milliseconds()},
		})
		return CollectionsResponse{}, err
	}
	defer response.Body.Close()
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "zotero",
		Action:    "list_collections_request",
		Message:   fmt.Sprintf("HTTP Request: GET %s %q", request.URL.String(), response.Proto+" "+response.Status),
		Data: map[string]any{
			"status_code": response.StatusCode,
			"duration_ms": time.Since(started).Milliseconds(),
		},
	})
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return CollectionsResponse{}, err
	}
	if response.StatusCode >= 400 {
		return CollectionsResponse{}, fmt.Errorf("Zotero API error %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload []map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return CollectionsResponse{}, fmt.Errorf("Unexpected Zotero collections response.")
	}

	byKey := map[string]map[string]any{}
	for _, item := range payload {
		key, ok := item["key"].(string)
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		data, ok := item["data"].(map[string]any)
		if !ok {
			continue
		}
		byKey[key] = data
	}
	buildPathLabel := func(collectionKey string) string {
		names := []string{}
		currentKey := collectionKey
		for currentKey != "" {
			current, ok := byKey[currentKey]
			if !ok {
				break
			}
			name := strings.TrimSpace(stringAny(current["name"]))
			if name == "" {
				name = currentKey
			}
			names = append(names, name)
			parent, _ := current["parentCollection"].(string)
			currentKey = strings.TrimSpace(parent)
		}
		slices.Reverse(names)
		return strings.Join(names, " / ")
	}

	defaultKey := optionalString(settings.ZoteroCollectionKey)
	collections := make([]CollectionOption, 0, len(byKey))
	for collectionKey, data := range byKey {
		parentKey := optionalString(stringAny(data["parentCollection"]))
		collections = append(collections, CollectionOption{
			Key:       collectionKey,
			Name:      firstNonEmptyString(stringAny(data["name"]), collectionKey),
			PathLabel: buildPathLabel(collectionKey),
			ParentKey: parentKey,
			IsDefault: defaultKey != nil && collectionKey == *defaultKey,
		})
	}
	slices.SortFunc(collections, func(left, right CollectionOption) int {
		return strings.Compare(strings.ToLower(left.PathLabel), strings.ToLower(right.PathLabel))
	})
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "zotero",
		Action:    "list_collections_completed",
		Message:   fmt.Sprintf("Loaded %d Zotero collection(s)", len(collections)),
		Data:      map[string]any{"default_collection_key": defaultKey},
	})
	return CollectionsResponse{
		Collections:          collections,
		DefaultCollectionKey: defaultKey,
	}, nil
}

func SavePaper(settings config.Settings, paper store.Paper, classification store.Classification, collectionKey *string) (*string, error) {
	// 把一篇已分类论文保存到 Zotero，并返回创建出的 item key。
	prefix, err := libraryPrefix(settings)
	if err != nil {
		return nil, err
	}
	targetCollectionKey := collectionKey
	if targetCollectionKey == nil {
		targetCollectionKey = optionalString(settings.ZoteroCollectionKey)
	} else if strings.TrimSpace(*targetCollectionKey) == "" {
		targetCollectionKey = nil
	}
	payload := []map[string]any{buildItemPayload(paper, classification, targetCollectionKey)}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(apiBaseURL, "/")+"/"+prefix+"/items", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Zotero-API-Version", "3")
	request.Header.Set("Zotero-API-Key", settings.ZoteroAPIKey)
	request.Header.Set("Content-Type", "application/json")
	started := time.Now()
	response, err := httpClient.Do(request)
	if err != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "error",
			Component: "zotero",
			Action:    "save_paper_failed",
			Message:   fmt.Sprintf("HTTP Request: POST %s failed", request.URL.String()),
			Error:     err.Error(),
			Data: map[string]any{
				"paper_title":    paper.Title,
				"paper_url":      paper.URL,
				"collection_key": targetCollectionKey,
				"duration_ms":    time.Since(started).Milliseconds(),
			},
		})
		return nil, err
	}
	defer response.Body.Close()
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "zotero",
		Action:    "save_paper_request",
		Message:   fmt.Sprintf("HTTP Request: POST %s %q", request.URL.String(), response.Proto+" "+response.Status),
		Data: map[string]any{
			"paper_title":    paper.Title,
			"paper_url":      paper.URL,
			"collection_key": targetCollectionKey,
			"status_code":    response.StatusCode,
			"duration_ms":    time.Since(started).Milliseconds(),
		},
	})
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("Zotero API error %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var payloadResponse map[string]any
	if err := json.Unmarshal(responseBody, &payloadResponse); err != nil {
		return nil, err
	}
	successful, _ := payloadResponse["successful"].(map[string]any)
	for _, value := range successful {
		itemInfo, ok := value.(map[string]any)
		if !ok {
			continue
		}
		itemKey := optionalString(stringAny(itemInfo["key"]))
		if itemKey != nil {
			_, _ = logging.WriteDefault(logging.Event{
				Level:     "info",
				Component: "zotero",
				Action:    "save_paper_completed",
				Message:   "Saved paper to Zotero",
				Data: map[string]any{
					"paper_title":    paper.Title,
					"paper_url":      paper.URL,
					"collection_key": targetCollectionKey,
					"item_key":       itemKey,
				},
			})
			return itemKey, nil
		}
	}
	return nil, nil
}

func buildItemPayload(paper store.Paper, classification store.Classification, collectionKey *string) map[string]any {
	creators := []map[string]string{}
	for _, author := range paper.Authors {
		parts := strings.Fields(strings.ReplaceAll(author, ",", " "))
		if len(parts) == 0 {
			continue
		}
		creators = append(creators, map[string]string{
			"creatorType": "author",
			"firstName":   strings.Join(parts[:maxInt(len(parts)-1, 0)], " "),
			"lastName":    parts[len(parts)-1],
		})
	}
	tags := []map[string]string{
		{"tag": "scirssagent"},
		{"tag": classification.Relevance},
	}
	payload := map[string]any{
		"itemType":         "journalArticle",
		"title":            paper.Title,
		"creators":         creators,
		"abstractNote":     valueOrEmpty(paper.Abstract),
		"publicationTitle": firstNonEmptyString(valueOrEmpty(paper.Journal), valueOrEmpty(paper.FeedTitle)),
		"date":             valueOrEmpty(paper.PublishedDate),
		"DOI":              valueOrEmpty(paper.DOI),
		"url":              paper.URL,
		"tags":             tags,
	}
	if collectionKey != nil {
		payload["collections"] = []string{*collectionKey}
	}
	return payload
}

func libraryPrefix(settings config.Settings) (string, error) {
	if strings.TrimSpace(settings.ZoteroAPIKey) == "" {
		return "", fmt.Errorf("SCIRSS_ZOTERO_API_KEY is not configured. Add it to your .env file first.")
	}
	if strings.TrimSpace(settings.ZoteroLibraryID) == "" {
		return "", fmt.Errorf("SCIRSS_ZOTERO_LIBRARY_ID is not configured. Add your Zotero user or group library ID to the .env file.")
	}
	libraryType := strings.ToLower(strings.TrimSpace(settings.ZoteroLibraryType))
	if libraryType != "user" && libraryType != "group" {
		return "", fmt.Errorf("SCIRSS_ZOTERO_LIBRARY_TYPE must be 'user' or 'group'. Check the .env setting before saving to Zotero.")
	}
	return libraryType + "s/" + settings.ZoteroLibraryID, nil
}

func optionalString(value string) *string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return nil
	}
	return &clean
}

func firstNonEmptyString(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func stringAny(value any) string {
	if value == nil {
		return ""
	}
	if cast, ok := value.(string); ok {
		return cast
	}
	return fmt.Sprintf("%v", value)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
