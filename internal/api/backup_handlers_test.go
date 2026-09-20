package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestBackupJobListAndDownload(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	db, err := sqlite.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	now, journal := time.Now().UTC(), "Nature"
	paperID, _, err := db.UpsertPaper(sqlite.Paper{SourceURL: "https://nature.com/nature.rss", Journal: &journal, Title: "Backup article", URL: "https://nature.com/article"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveClassification(paperID, sqlite.Classification{Relevance: "direct", Model: "test", TopicTags: []string{}, RecommendedAction: "read"}, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settings.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.ProfilePath, []byte(`{"meta":{"name":"Test","version":1,"created_at":"2026-09-20T00:00:00Z","updated_at":"2026-09-20T00:00:00Z","source_description":"test"},"scope":"Research","relevance_rules":{"direct":[],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.FeedsPath, []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	restore := stubAPIGlobals(t)
	defer restore()
	handler := newTestHandler(t, settings)
	launch := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/backups", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	handler.ServeHTTP(launch, request)
	if launch.Code != http.StatusOK {
		t.Fatalf("launch %d: %s", launch.Code, launch.Body.String())
	}
	var payload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(launch.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, payload.Job.ID)
	job, ok := jobByID(payload.Job.ID)
	if !ok || job.Status != "completed" {
		t.Fatalf("backup job %#v", job)
	}
	var result struct {
		Backup struct {
			ID string `json:"id"`
		} `json:"backup"`
	}
	raw, _ := json.Marshal(job.Result)
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	listing := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/admin/backups", nil)
	listRequest.RemoteAddr = "127.0.0.1:1234"
	handler.ServeHTTP(listing, listRequest)
	if listing.Code != 200 || !contains(listing.Body.String(), result.Backup.ID) {
		t.Fatalf("list %d: %s", listing.Code, listing.Body.String())
	}
	download := httptest.NewRecorder()
	downloadRequest := httptest.NewRequest(http.MethodGet, "/api/admin/backups/"+result.Backup.ID+"/download", nil)
	downloadRequest.RemoteAddr = "127.0.0.1:1234"
	handler.ServeHTTP(download, downloadRequest)
	if download.Code != 200 || download.Header().Get("Content-Type") != "application/zip" || download.Body.Len() == 0 {
		t.Fatalf("download %d headers=%v", download.Code, download.Header())
	}
	reader, err := zip.NewReader(bytes.NewReader(download.Body.Bytes()), int64(download.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	restoredSettings := testSettings(t.TempDir())
	if err := os.MkdirAll(restoredSettings.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name != "literature.sqlite" && file.Name != "classification_profile.json" && file.Name != "rss_feeds.json" {
			continue
		}
		input, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(input)
		input.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(restoredSettings.DataDir, file.Name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	restored := newTestHandler(t, restoredSettings)
	report := httptest.NewRecorder()
	restored.ServeHTTP(report, httptest.NewRequest(http.MethodGet, "/api/report/latest", nil))
	if report.Code != 200 || !contains(report.Body.String(), `"journal_display":"Nature"`) || !contains(report.Body.String(), "Backup article") {
		t.Fatalf("restored app report %d: %s", report.Code, report.Body.String())
	}
	outside := httptest.NewRecorder()
	outsideRequest := httptest.NewRequest(http.MethodGet, "/api/admin/backups/%2e%2e%2fsecrets/download", nil)
	outsideRequest.RemoteAddr = "127.0.0.1:1234"
	handler.ServeHTTP(outside, outsideRequest)
	if outside.Code != http.StatusNotFound {
		t.Fatalf("path traversal status %d", outside.Code)
	}
}

func TestBackupRejectsMissingDatabase(t *testing.T) {
	handler := newTestHandler(t, testSettings(t.TempDir()))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/backups", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
}

func TestBackupEndpointsRejectRemoteClients(t *testing.T) {
	handler := newTestHandler(t, testSettings(t.TempDir()))
	request := httptest.NewRequest(http.MethodGet, "/api/admin/backups", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("remote backup status=%d", response.Code)
	}
}
