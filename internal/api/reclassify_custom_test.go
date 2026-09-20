package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestCustomReclassifyPreviewAndStaleFingerprint(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	if err := os.MkdirAll(settings.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	now, journal := time.Now().In(time.Local), "Nature"
	if _, _, err := db.UpsertPaper(sqlite.Paper{SourceURL: "https://nature.com/nature.rss", Journal: &journal, Title: "Paper", URL: "https://nature.com/paper"}, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	restore := stubAPIGlobals(t)
	defer restore()
	handler := newTestHandler(t, settings)
	optionsResponse := httptest.NewRecorder()
	handler.ServeHTTP(optionsResponse, httptest.NewRequest(http.MethodGet, "/api/admin/journals", nil))
	if optionsResponse.Code != 200 || !contains(optionsResponse.Body.String(), "Nature") {
		t.Fatalf("options %d: %s", optionsResponse.Code, optionsResponse.Body.String())
	}
	var options struct {
		Journals []struct {
			Key   string `json:"key"`
			Label string `json:"label"`
		} `json:"journals"`
	}
	if err := json.Unmarshal(optionsResponse.Body.Bytes(), &options); err != nil {
		t.Fatal(err)
	}
	var key string
	for _, option := range options.Journals {
		if option.Label == "Nature" {
			key = option.Key
		}
	}
	if key == "" {
		t.Fatal("canonical Nature journal missing")
	}
	previewURL := "/api/admin/reclassify?scope=custom&journal_key=" + key + "&date_from=" + now.Format("2006-01-02")
	previewResponse := httptest.NewRecorder()
	handler.ServeHTTP(previewResponse, httptest.NewRequest(http.MethodGet, previewURL, nil))
	if previewResponse.Code != 200 || !contains(previewResponse.Body.String(), `"total":1`) || !contains(previewResponse.Body.String(), `"unclassified":1`) {
		t.Fatalf("preview %d: %s", previewResponse.Code, previewResponse.Body.String())
	}
	filterJSON, _ := json.Marshal(map[string]any{"journal_keys": []string{key}, "date_from": now.Format("2006-01-02"), "date_to": ""})
	postPreview := httptest.NewRecorder()
	handler.ServeHTTP(postPreview, httptest.NewRequest(http.MethodPost, "/api/admin/reclassify/preview", bytes.NewReader(filterJSON)))
	if postPreview.Code != 200 || !contains(postPreview.Body.String(), `"total":1`) {
		t.Fatalf("POST preview %d: %s", postPreview.Code, postPreview.Body.String())
	}
	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/admin/reclassify?scope=custom&journal_key="+key+"&date_from=2026-02-30", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid preview status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	var stale map[string]any
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &stale); err != nil {
		t.Fatal(err)
	}
	requestBody, _ := json.Marshal(map[string]any{"scope": "custom", "journal_keys": []string{key}, "date_from": now.Format("2006-01-02"), "fingerprint": "stale"})
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/admin/reclassify", bytes.NewReader(requestBody)))
	if post.Code != http.StatusConflict || !contains(post.Body.String(), "fingerprint") && !contains(post.Body.String(), "changed") {
		t.Fatalf("stale launch %d: %s", post.Code, post.Body.String())
	}
}
