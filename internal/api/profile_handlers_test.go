package api

import (
	"database/sql"
	"encoding/json"
	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
	zoterosvc "github.com/yyngfive/scirssagent/internal/zotero"
	"io"
	_ "modernc.org/sqlite"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestReadOnlyBootstrapEndpoints(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Test","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"test"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`)
	seedReadOnlyFixture(t, settings.DatabasePath)
	handler := newTestHandler(t, settings)

	profileRecorder := httptest.NewRecorder()
	handler.ServeHTTP(profileRecorder, httptest.NewRequest(http.MethodGet, "/api/profile/current", nil))
	if profileRecorder.Code != http.StatusOK || !contains(profileRecorder.Body.String(), `"name":"Test"`) {
		t.Fatalf("profile response = %d %s", profileRecorder.Code, profileRecorder.Body.String())
	}

	reportRecorder := httptest.NewRecorder()
	handler.ServeHTTP(reportRecorder, httptest.NewRequest(http.MethodGet, "/api/report/latest", nil))
	if reportRecorder.Code != http.StatusOK || !contains(reportRecorder.Body.String(), `"total":1`) || !contains(reportRecorder.Body.String(), `"title":"API paper"`) {
		t.Fatalf("report response = %d %s", reportRecorder.Code, reportRecorder.Body.String())
	}

	feedbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(feedbackRecorder, httptest.NewRequest(http.MethodGet, "/api/feedback", nil))
	if feedbackRecorder.Code != http.StatusOK || !contains(feedbackRecorder.Body.String(), `"corrected_relevance":"direct"`) {
		t.Fatalf("feedback response = %d %s", feedbackRecorder.Code, feedbackRecorder.Body.String())
	}

	proposalsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(proposalsRecorder, httptest.NewRequest(http.MethodGet, "/api/profile/proposals", nil))
	if proposalsRecorder.Code != http.StatusOK || !contains(proposalsRecorder.Body.String(), `"summary":"Proposal summary"`) {
		t.Fatalf("proposal list response = %d %s", proposalsRecorder.Code, proposalsRecorder.Body.String())
	}

	proposalRecorder := httptest.NewRecorder()
	handler.ServeHTTP(proposalRecorder, httptest.NewRequest(http.MethodGet, "/api/profile/proposals/1", nil))
	if proposalRecorder.Code != http.StatusOK || !contains(proposalRecorder.Body.String(), `"source_feedback_ids":[1,2]`) {
		t.Fatalf("proposal detail response = %d %s", proposalRecorder.Code, proposalRecorder.Body.String())
	}

	jobsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(jobsRecorder, httptest.NewRequest(http.MethodGet, "/api/admin/jobs", nil))
	if jobsRecorder.Code != http.StatusOK || strings.TrimSpace(jobsRecorder.Body.String()) != "[]" {
		t.Fatalf("jobs response = %d %s", jobsRecorder.Code, jobsRecorder.Body.String())
	}
}

func TestPaperAbstractImageProxyUsesPaperReferer(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	seedReadOnlyFixture(t, settings.DatabasePath)
	db, err := sql.Open("sqlite", settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	execSQLite(t, db, `
UPDATE papers
SET raw_json = '{"_abstract_html":"<img src=\"https://images.example/figure.png\">","_abstract_images":[{"src":"https://images.example/figure.png"}]}'
WHERE id = 1
`)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var seenReferer string
	var seenUserAgent string
	previousClient := abstractImageHTTPClient
	abstractImageHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		seenReferer = request.Header.Get("Referer")
		seenUserAgent = request.Header.Get("User-Agent")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(strings.NewReader("png")),
			Request:    request,
		}, nil
	})}
	t.Cleanup(func() { abstractImageHTTPClient = previousClient })

	handler := newTestHandler(t, settings)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/papers/1/abstract-image?src=https%3A%2F%2Fimages.example%2Ffigure.png", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "png" {
		t.Fatalf("image proxy = %d %q", recorder.Code, recorder.Body.String())
	}
	if seenReferer != "https://example.com/api-paper" || !strings.Contains(seenUserAgent, "Mozilla/5.0") {
		t.Fatalf("headers referer=%q ua=%q", seenReferer, seenUserAgent)
	}

	missingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingRecorder, httptest.NewRequest(http.MethodGet, "/api/papers/1/abstract-image?src=https%3A%2F%2Fimages.example%2Fother.png", nil))
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("unlisted image status = %d", missingRecorder.Code)
	}
}

func TestProfileCurrentPutUpdatesExistingProfile(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":2,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`)
	handler := newTestHandler(t, settings)

	body := `{
		"meta":{"name":"Edited profile","version":1,"created_at":"2026-05-20T00:00:00Z","updated_at":"2026-05-20T00:00:00Z","source_description":"edited"},
		"scope":"RNA biology and splicing",
		"relevance_rules":{"direct":["RNA","Splicing"],"indirect":["Protein complexes"],"unrelated":["Plant biology"]},
		"topic_taxonomy":[{"id":"rna_bio","label":"RNA Bio"}],
		"few_shots":[{"title":"Example paper","relevance":"direct","tags":["rna_bio"],"rationale":"Tracks RNA mechanisms."}]
	}`
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/profile/current", strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("put profile = %d %s", recorder.Code, recorder.Body.String())
	}
	if !contains(recorder.Body.String(), `"name":"Edited profile"`) || !contains(recorder.Body.String(), `"version":3`) {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
	if !contains(recorder.Body.String(), `"created_at":"2026-05-10T00:00:00Z"`) {
		t.Fatalf("expected created_at preserved: %s", recorder.Body.String())
	}
	if !contains(recorder.Body.String(), `"source_description":"current"`) {
		t.Fatalf("expected source_description preserved: %s", recorder.Body.String())
	}
	if !contains(recorder.Body.String(), `"topic_taxonomy":[]`) || !contains(recorder.Body.String(), `"few_shots":[]`) {
		t.Fatalf("expected deprecated profile fields cleared in response: %s", recorder.Body.String())
	}

	saved, err := os.ReadFile(settings.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(saved), `"version": 3`) || !contains(string(saved), `"name": "Edited profile"`) {
		t.Fatalf("saved profile = %s", saved)
	}
	if !contains(string(saved), `"topic_taxonomy": []`) || !contains(string(saved), `"few_shots": []`) {
		t.Fatalf("expected deprecated profile fields cleared on disk: %s", saved)
	}
}

func TestProfileCurrentPutRejectsInvalidOrMissingProfile(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	handler := newTestHandler(t, settings)

	missingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingRecorder, httptest.NewRequest(http.MethodPut, "/api/profile/current", strings.NewReader(`{}`)))
	if missingRecorder.Code != http.StatusBadRequest || !contains(missingRecorder.Body.String(), `No classification profile exists yet.`) {
		t.Fatalf("missing profile response = %d %s", missingRecorder.Code, missingRecorder.Body.String())
	}

	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":1,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`)
	invalidRecorder := httptest.NewRecorder()
	invalidBody := `{"meta":{"name":"","version":1,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"","relevance_rules":{"direct":[],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`
	handler.ServeHTTP(invalidRecorder, httptest.NewRequest(http.MethodPut, "/api/profile/current", strings.NewReader(invalidBody)))
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid profile response = %d %s", invalidRecorder.Code, invalidRecorder.Body.String())
	}
}

func TestFeedbackReadAndDeleteMutationAPIs(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	seedReadOnlyFixture(t, settings.DatabasePath)
	handler := newTestHandler(t, settings)

	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, httptest.NewRequest(http.MethodPost, "/api/feedback", strings.NewReader(`{"paper_id":1,"corrected_relevance":"direct","note":"Promote."}`)))
	if createRecorder.Code != http.StatusOK || !contains(createRecorder.Body.String(), `"original_relevance":"indirect"`) {
		t.Fatalf("create feedback = %d %s", createRecorder.Code, createRecorder.Body.String())
	}

	readRecorder := httptest.NewRecorder()
	handler.ServeHTTP(readRecorder, httptest.NewRequest(http.MethodPost, "/api/papers/1/read", nil))
	if readRecorder.Code != http.StatusOK || !contains(readRecorder.Body.String(), `"paper_id":1`) {
		t.Fatalf("mark read = %d %s", readRecorder.Code, readRecorder.Body.String())
	}
	firstReadPayload := readRecorder.Body.String()
	readAgainRecorder := httptest.NewRecorder()
	handler.ServeHTTP(readAgainRecorder, httptest.NewRequest(http.MethodPost, "/api/papers/1/read", nil))
	if readAgainRecorder.Code != http.StatusOK || readAgainRecorder.Body.String() != firstReadPayload {
		t.Fatalf("mark read again = %d %s", readAgainRecorder.Code, readAgainRecorder.Body.String())
	}
	unreadRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unreadRecorder, httptest.NewRequest(http.MethodPost, "/api/papers/1/read", strings.NewReader(`{"read":false}`)))
	if unreadRecorder.Code != http.StatusOK || !contains(unreadRecorder.Body.String(), `"read_at":null`) {
		t.Fatalf("mark unread = %d %s", unreadRecorder.Code, unreadRecorder.Body.String())
	}

	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/feedback/1", nil))
	if deleteRecorder.Code != http.StatusOK || !contains(deleteRecorder.Body.String(), `"deleted":true`) {
		t.Fatalf("delete feedback = %d %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

func TestBootstrapProposalGenerateAndZoteroBridgeAPIs(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()
	seedReadOnlyFixture(t, settings.DatabasePath)

	bootstrapProfileFunc = func(_ config.Settings, interestDescription string, name *string, progress jobruntime.ProgressFunc) (map[string]any, error) {
		if progress != nil {
			progress(jobruntime.StepProgress("profile.bootstrap.generating", "profile-bootstrap", 2, 4, "Step 2/4 (50%): Generating initial profile proposal."))
		}
		result := map[string]any{"proposal_id": 1}
		if name != nil {
			result["name"] = *name
		}
		_ = interestDescription
		return result, nil
	}
	generateProfileProposalFunc = func(_ config.Settings, progress jobruntime.ProgressFunc) (map[string]any, error) {
		if progress != nil {
			progress(jobruntime.StepProgress("profile.proposal.collecting_feedback", "profile-proposal", 1, 4, "Step 1/4 (25%): Collecting feedback and current profile context."))
			progress(jobruntime.StepProgress("profile.proposal.generating", "profile-proposal", 2, 4, "Step 2/4 (50%): Generating profile proposal."))
		}
		return map[string]any{"proposal_id": 2, "state": "pending"}, nil
	}
	listZoteroCollectionsFunc = func(_ config.Settings) (zoterosvc.CollectionsResponse, error) {
		return zoterosvc.CollectionsResponse{
			Collections: []zoterosvc.CollectionOption{{Key: "COLL-1", Name: "Inbox", PathLabel: "Inbox"}},
		}, nil
	}
	savePaperToZoteroFunc = func(_ config.Settings, paper store.Paper, classification store.Classification, collectionKey *string) (*string, error) {
		if paper.ID != 1 || classification.Relevance != "indirect" {
			t.Fatalf("unexpected zotero save input: paper=%#v classification=%#v", paper, classification)
		}
		if collectionKey == nil || *collectionKey != "COLL-1" {
			t.Fatalf("unexpected collection key: %#v", collectionKey)
		}
		itemKey := "ITEM-1"
		return &itemKey, nil
	}

	handler := newTestHandler(t, settings)

	bootstrapRecorder := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/bootstrap", strings.NewReader(`{"interest_description":"RNA biology","name":"Alice"}`)))
	if bootstrapRecorder.Code != http.StatusOK || !contains(bootstrapRecorder.Body.String(), `"job_type":"profile-bootstrap"`) {
		t.Fatalf("bootstrap launch = %d %s", bootstrapRecorder.Code, bootstrapRecorder.Body.String())
	}
	var bootstrapPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(bootstrapRecorder.Body.Bytes(), &bootstrapPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, bootstrapPayload.Job.ID)
	bootstrapJob, ok := jobByID(bootstrapPayload.Job.ID)
	if !ok || bootstrapJob.Result["proposal_id"] != 1.0 && bootstrapJob.Result["proposal_id"] != 1 {
		t.Fatalf("unexpected bootstrap job result: %#v", bootstrapJob)
	}

	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":1,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`)

	generateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(generateRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/proposals/generate", nil))
	if generateRecorder.Code != http.StatusOK || !contains(generateRecorder.Body.String(), `"job_type":"profile-proposal"`) {
		t.Fatalf("proposal generate launch = %d %s", generateRecorder.Code, generateRecorder.Body.String())
	}
	var generatePayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(generateRecorder.Body.Bytes(), &generatePayload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, generatePayload.Job.ID)
	generateJob, ok := jobByID(generatePayload.Job.ID)
	if !ok || generateJob.Result["proposal_id"] != 2.0 && generateJob.Result["proposal_id"] != 2 {
		t.Fatalf("unexpected proposal generate job result: %#v", generateJob)
	}

	collectionsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(collectionsRecorder, httptest.NewRequest(http.MethodGet, "/api/zotero/collections", nil))
	if collectionsRecorder.Code != http.StatusOK || !contains(collectionsRecorder.Body.String(), `"key":"COLL-1"`) {
		t.Fatalf("zotero collections = %d %s", collectionsRecorder.Code, collectionsRecorder.Body.String())
	}

	saveRecorder := httptest.NewRecorder()
	handler.ServeHTTP(saveRecorder, httptest.NewRequest(http.MethodPost, "/api/zotero/save/1", strings.NewReader(`{"collection_key":"COLL-1"}`)))
	if saveRecorder.Code != http.StatusOK || !contains(saveRecorder.Body.String(), `"item_key":"ITEM-1"`) {
		t.Fatalf("zotero save = %d %s", saveRecorder.Code, saveRecorder.Body.String())
	}
}

func TestProfileProposalApplyRejectAndReclassifyAPI(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":1,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`)
	seedReadOnlyFixture(t, settings.DatabasePath)

	reclassifyPaperIDsFunc = func(_ config.Settings, paperIDs []int64, progress jobruntime.ProgressFunc) (int, error) {
		if progress != nil {
			progress(jobruntime.PercentProgress("pipeline.classifier.classifying", "classification", 1, 1, "Classifying papers 1/1 (100%)."))
		}
		if len(paperIDs) == 0 {
			return 0, nil
		}
		return len(paperIDs), nil
	}
	selectReclassifyPaperIDsFunc = func(_ config.Settings, scope string, limit int) ([]int64, error) {
		return []int64{1}, nil
	}
	rebuildLatestReportFunc = func(_ config.Settings, progress jobruntime.ProgressFunc) (int, error) {
		if progress != nil {
			progress(jobruntime.ProgressUpdate{MessageKey: "pipeline.report.refreshing", Message: "Refreshing the latest report from SQLite.", Stage: "report"})
		}
		return 1, nil
	}

	handler := newTestHandler(t, settings)

	applyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(applyRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/proposals/1/apply", nil))
	if applyRecorder.Code != http.StatusOK || !contains(applyRecorder.Body.String(), `"state":"applied"`) || !contains(applyRecorder.Body.String(), `"applied_version":2`) {
		t.Fatalf("apply proposal = %d %s", applyRecorder.Code, applyRecorder.Body.String())
	}
	appliedProfile, err := os.ReadFile(settings.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(appliedProfile), `"version": 2`) {
		t.Fatalf("applied profile = %s", appliedProfile)
	}

	feedbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(feedbackRecorder, httptest.NewRequest(http.MethodGet, "/api/feedback", nil))
	if feedbackRecorder.Code != http.StatusOK || !contains(feedbackRecorder.Body.String(), `"used_in_profile":true`) {
		t.Fatalf("feedback after apply = %d %s", feedbackRecorder.Code, feedbackRecorder.Body.String())
	}
	if used := feedbackStateCount(t, settings.DatabasePath, "used"); used != 2 {
		t.Fatalf("legacy proposal should mark all source feedback used, got %d", used)
	}

	rejectRecorder := httptest.NewRecorder()
	handler.ServeHTTP(rejectRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/proposals/1/reject", nil))
	if rejectRecorder.Code != http.StatusOK || !contains(rejectRecorder.Body.String(), `"state":"rejected"`) {
		t.Fatalf("reject proposal = %d %s", rejectRecorder.Code, rejectRecorder.Body.String())
	}

	reclassifyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(reclassifyRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/reclassify", strings.NewReader(`{"scope":"all","limit":50}`)))
	if reclassifyRecorder.Code != http.StatusOK || !contains(reclassifyRecorder.Body.String(), `"job_type":"reclassify"`) {
		t.Fatalf("reclassify launch = %d %s", reclassifyRecorder.Code, reclassifyRecorder.Body.String())
	}
	var payload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(reclassifyRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, payload.Job.ID)
	job, ok := jobByID(payload.Job.ID)
	if !ok || job.Result["reclassified"] != float64(1) && job.Result["reclassified"] != 1 {
		t.Fatalf("unexpected reclassify job payload: %#v", job)
	}
}

func TestCompactProfileProposalApplyUsesAcceptedChanges(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":1,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":["Background"],"unrelated":["Plant biology"]},"topic_taxonomy":[],"few_shots":[]}`)
	seedCompactProposalFixture(t, settings.DatabasePath)

	reclassifiedPaperIDs := []int64(nil)
	reclassifyPaperIDsFunc = func(_ config.Settings, paperIDs []int64, _ jobruntime.ProgressFunc) (int, error) {
		reclassifiedPaperIDs = append([]int64(nil), paperIDs...)
		return len(paperIDs), nil
	}
	rebuildLatestReportFunc = func(_ config.Settings, _ jobruntime.ProgressFunc) (int, error) {
		return 1, nil
	}

	handler := newTestHandler(t, settings)
	body := `{"accepted_change_ids":["change-1"],"rejected_change_ids":["change-2"]}`
	applyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(applyRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/proposals/21/apply", strings.NewReader(body)))
	if applyRecorder.Code != http.StatusOK || !contains(applyRecorder.Body.String(), `"state":"applied"`) || !contains(applyRecorder.Body.String(), `"status":"accepted"`) || !contains(applyRecorder.Body.String(), `"status":"rejected"`) {
		t.Fatalf("compact apply = %d %s", applyRecorder.Code, applyRecorder.Body.String())
	}

	appliedProfile, err := os.ReadFile(settings.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(appliedProfile), `"RNA chemistry"`) || !contains(string(appliedProfile), `"Plant biology"`) {
		t.Fatalf("applied compact profile = %s", appliedProfile)
	}
	if contains(string(appliedProfile), `"RNA",`) {
		t.Fatalf("expected accepted rewrite to replace the old direct rule: %s", appliedProfile)
	}
	if len(reclassifiedPaperIDs) != 1 || reclassifiedPaperIDs[0] != 1 {
		t.Fatalf("expected only accepted feedback paper reclassified, got %#v", reclassifiedPaperIDs)
	}
	if used := feedbackStateCount(t, settings.DatabasePath, "used"); used != 1 {
		t.Fatalf("expected only accepted feedback to be used, got %d", used)
	}
	if open := feedbackStateCount(t, settings.DatabasePath, "open"); open != 1 {
		t.Fatalf("expected rejected feedback to remain open, got %d", open)
	}
}

func TestCompactProfileProposalApplyRejectsVersionMismatch(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":2,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":["Background"],"unrelated":["Plant biology"]},"topic_taxonomy":[],"few_shots":[]}`)
	seedCompactProposalFixture(t, settings.DatabasePath)

	handler := newTestHandler(t, settings)
	body := `{"accepted_change_ids":["change-1"],"rejected_change_ids":[]}`
	applyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(applyRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/proposals/21/apply", strings.NewReader(body)))
	if applyRecorder.Code != http.StatusConflict || !contains(applyRecorder.Body.String(), "Regenerate the proposal") {
		t.Fatalf("version mismatch apply = %d %s", applyRecorder.Code, applyRecorder.Body.String())
	}
}
