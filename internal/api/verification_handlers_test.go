package api

import (
	"encoding/json"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	_ "modernc.org/sqlite"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminRunWaitsForCloudflareVerificationAndResumes(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runCalls := 0
	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, progress jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		runCalls++
		if opts.VerifyFeedHost == nil {
			t.Fatal("expected host verification callback")
		}
		verification := opts.VerifyFeedHost([]feeds.VerificationRequest{{
			URL:    "https://chemrxiv.org/action/showFeed?type=latest&format=rss",
			Target: "cloudflare",
			Reason: "challenge",
		}})
		if string(verification.FeedBodies["https://chemrxiv.org/action/showFeed?type=latest&format=rss"]) != "<rdf:RDF />" {
			t.Fatalf("unexpected verification body: %#v", verification.FeedBodies)
		}
		return jobruntime.RunSummary{
			Fetched:    1,
			Inserted:   1,
			Updated:    0,
			Classified: 1,
			Errors:     nil,
		}, nil
	}

	startVerificationFlowFunc = func(_ config.Settings, pending *pendingVerification) error {
		if pending == nil {
			t.Fatalf("expected pending verification")
		}
		if pending.FeedURL != "https://chemrxiv.org/action/showFeed?type=latest&format=rss" {
			t.Fatalf("unexpected feed url: %q", pending.FeedURL)
		}
		return nil
	}
	handler := newTestHandler(t, testSettings(root))

	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run launch = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
	var runPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &runPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")

	job, ok := jobByID(runPayload.Job.ID)
	if !ok || !job.VerificationRequired || job.VerificationTarget != "cloudflare" {
		t.Fatalf("unexpected waiting job: %#v", job)
	}
	pending, ok := pendingVerificationForJob(runPayload.Job.ID, "https://chemrxiv.org/action/showFeed?type=latest&format=rss")
	if !ok {
		t.Fatalf("expected pending verification")
	}

	startBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://chemrxiv.org/action/showFeed?type=latest&format=rss"}`
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/start", strings.NewReader(startBody)))
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("verification start = %d %s", startRecorder.Code, startRecorder.Body.String())
	}
	callbackBody := `{"verification_id":"` + pending.ID + `","status":"success","content_type":"application/xml","feed_xml":"<rdf:RDF />","error":""}`
	callbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(callbackRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/callback", strings.NewReader(callbackBody)))
	if callbackRecorder.Code != http.StatusOK {
		t.Fatalf("verification callback = %d %s", callbackRecorder.Code, callbackRecorder.Body.String())
	}

	waitForJobCompletion(t, runPayload.Job.ID)
	completedJob, ok := jobByID(runPayload.Job.ID)
	if !ok || completedJob.Status != "completed" {
		t.Fatalf("unexpected completed job: %#v", completedJob)
	}
	if runCalls != 1 {
		t.Fatalf("runCalls = %d", runCalls)
	}
}

func TestAdminRunCanFallbackToBrowserManualXMLAfterVerifierAbort(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	opened := ""
	openExternalTargetFunc = func(target string) error {
		opened = target
		return nil
	}
	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if opts.VerifyFeedHost == nil {
			t.Fatal("expected host verification callback")
		}
		verification := opts.VerifyFeedHost([]feeds.VerificationRequest{{
			URL:     "https://www.cell.com/cell/current.rss",
			Target:  "cloudflare",
			Reason:  "challenge",
			Journal: "Cell",
		}})
		if string(verification.FeedBodies["https://www.cell.com/cell/current.rss"]) != "<rss><channel><title>Cell</title><item><title>Paper</title><link>https://example.com/paper</link></item></channel></rss>" {
			t.Fatalf("unexpected verification result: %#v", verification)
		}
		return jobruntime.RunSummary{
			Fetched:    1,
			Inserted:   1,
			Updated:    0,
			Classified: 1,
			Errors:     nil,
		}, nil
	}

	startVerificationFlowFunc = func(_ config.Settings, pending *pendingVerification) error {
		if pending == nil {
			t.Fatalf("expected pending verification")
		}
		if pending.FeedURL != "https://www.cell.com/cell/current.rss" {
			t.Fatalf("unexpected feed url: %q", pending.FeedURL)
		}
		return nil
	}
	handler := newTestHandler(t, testSettings(root))

	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run launch = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
	var runPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &runPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")
	pending, ok := pendingVerificationForJob(runPayload.Job.ID, "https://www.cell.com/cell/current.rss")
	if !ok {
		t.Fatalf("expected pending verification")
	}

	startBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss"}`
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/start", strings.NewReader(startBody)))
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("verification start = %d %s", startRecorder.Code, startRecorder.Body.String())
	}
	callbackBody := `{"verification_id":"` + pending.ID + `","status":"aborted","content_type":"","feed_xml":"","error":"Cloudflare challenge was not completed before the window closed."}`
	callbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(callbackRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/callback", strings.NewReader(callbackBody)))
	if callbackRecorder.Code != http.StatusOK {
		t.Fatalf("verification callback = %d %s", callbackRecorder.Code, callbackRecorder.Body.String())
	}

	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")
	waitingJob, ok := jobByID(runPayload.Job.ID)
	if !ok || waitingJob.VerificationMethod != verificationMethodNativeWebview {
		t.Fatalf("expected waiting job after verifier abort: %#v", waitingJob)
	}

	browserRecorder := httptest.NewRecorder()
	handler.ServeHTTP(browserRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/browser", strings.NewReader(startBody)))
	if browserRecorder.Code != http.StatusOK {
		t.Fatalf("verification browser = %d %s", browserRecorder.Code, browserRecorder.Body.String())
	}
	if opened != "https://www.cell.com/cell/current.rss" {
		t.Fatalf("unexpected opened target: %q", opened)
	}
	waitingJob, ok = jobByID(runPayload.Job.ID)
	if !ok || waitingJob.VerificationMethod != verificationMethodBrowserManual {
		t.Fatalf("expected browser fallback waiting job: %#v", waitingJob)
	}

	manualBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss","feed_xml":"<rss><channel><title>Cell</title><item><title>Paper</title><link>https://example.com/paper</link></item></channel></rss>"}`
	manualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manualRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/manual-submit", strings.NewReader(manualBody)))
	if manualRecorder.Code != http.StatusOK {
		t.Fatalf("verification manual submit = %d %s", manualRecorder.Code, manualRecorder.Body.String())
	}

	waitForJobCompletion(t, runPayload.Job.ID)
}

func TestAdminRunReusesHostScopedVerificationForMultipleFeeds(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	settings := testSettings(root)
	if _, err := markVerificationHostSessionVerified(settings, "pubs.acs.org", verificationVerifierKindNativeWebView); err != nil {
		t.Fatal(err)
	}

	runCalls := 0
	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		runCalls++
		if opts.VerifyFeedHost == nil {
			t.Fatal("expected host verification callback")
		}
		verification := opts.VerifyFeedHost([]feeds.VerificationRequest{
			{URL: "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat", Target: "cloudflare", Reason: "challenge", Journal: "JACS"},
			{URL: "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=ancham", Target: "cloudflare", Reason: "challenge", Journal: "Analytical Chemistry"},
		})
		if len(verification.FeedBodies) != 2 {
			t.Fatalf("expected two verified ACS feeds, got %#v", verification.FeedBodies)
		}
		return jobruntime.RunSummary{Fetched: 2, Inserted: 2, Classified: 2}, nil
	}
	startVerificationFlowFunc = func(_ config.Settings, pending *pendingVerification) error {
		if pending == nil {
			t.Fatal("expected pending verification")
		}
		if pending.Host != "pubs.acs.org" || len(pending.FeedURLs) != 2 {
			t.Fatalf("unexpected ACS pending verification: %#v", pending)
		}
		go func() {
			pending.Result <- verificationResult{
				Status: "success",
				FeedBodies: map[string][]byte{
					"https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat": []byte("<rss><channel><title>JACS</title><item><title>One</title><link>https://example.com/1</link></item></channel></rss>"),
					"https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=ancham": []byte("<rss><channel><title>Analytical Chemistry</title><item><title>Two</title><link>https://example.com/2</link></item></channel></rss>"),
				},
			}
		}()
		return nil
	}

	handler := newTestHandler(t, settings)
	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run launch = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
	var runPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &runPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, runPayload.Job.ID)
	if runCalls != 1 {
		t.Fatalf("runCalls = %d", runCalls)
	}
}

func TestVerificationCompleteRequiresCallbackXML(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if opts.VerifyFeedHost == nil {
			t.Fatal("expected host verification callback")
		}
		verification := opts.VerifyFeedHost([]feeds.VerificationRequest{{
			URL:     "https://www.cell.com/cell/current.rss",
			Target:  "cloudflare",
			Reason:  "challenge",
			Journal: "Cell",
		}})
		t.Fatalf("did not expect verification to finish: %#v", verification)
		return jobruntime.RunSummary{}, nil
	}
	startVerificationFlowFunc = func(_ config.Settings, _ *pendingVerification) error {
		return nil
	}

	handler := newTestHandler(t, testSettings(root))

	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run launch = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
	var runPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &runPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")

	startBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss"}`
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/start", strings.NewReader(startBody)))
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("verification start = %d %s", startRecorder.Code, startRecorder.Body.String())
	}

	completeBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss"}`
	completeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(completeRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/complete", strings.NewReader(completeBody)))
	if completeRecorder.Code != http.StatusBadRequest {
		t.Fatalf("verification complete = %d %s", completeRecorder.Code, completeRecorder.Body.String())
	}
	if !contains(completeRecorder.Body.String(), "has not returned RSS XML yet") {
		t.Fatalf("unexpected verification complete error: %s", completeRecorder.Body.String())
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")
}

func TestVerificationManualSubmitRejectsEmptyXML(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if opts.VerifyFeedHost == nil {
			t.Fatal("expected host verification callback")
		}
		verification := opts.VerifyFeedHost([]feeds.VerificationRequest{{
			URL:     "https://www.cell.com/cell/current.rss",
			Target:  "cloudflare",
			Reason:  "challenge",
			Journal: "Cell",
		}})
		t.Fatalf("did not expect verification to finish: %#v", verification)
		return jobruntime.RunSummary{}, nil
	}
	startVerificationFlowFunc = func(_ config.Settings, _ *pendingVerification) error {
		return nil
	}

	handler := newTestHandler(t, testSettings(root))
	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run launch = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
	var runPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &runPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")

	manualBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss","feed_xml":"   "}`
	manualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manualRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/manual-submit", strings.NewReader(manualBody)))
	if manualRecorder.Code != http.StatusBadRequest {
		t.Fatalf("verification manual submit = %d %s", manualRecorder.Code, manualRecorder.Body.String())
	}
	if !contains(manualRecorder.Body.String(), "Feed XML is required.") {
		t.Fatalf("unexpected manual submit error: %s", manualRecorder.Body.String())
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")
}

func TestVerificationManualSubmitRejectsMalformedXML(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if opts.VerifyFeedHost == nil {
			t.Fatal("expected host verification callback")
		}
		verification := opts.VerifyFeedHost([]feeds.VerificationRequest{{
			URL:     "https://www.cell.com/cell/current.rss",
			Target:  "cloudflare",
			Reason:  "challenge",
			Journal: "Cell",
		}})
		t.Fatalf("did not expect verification to finish: %#v", verification)
		return jobruntime.RunSummary{}, nil
	}
	startVerificationFlowFunc = func(_ config.Settings, _ *pendingVerification) error {
		return nil
	}

	handler := newTestHandler(t, testSettings(root))
	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run launch = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
	var runPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &runPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")

	manualBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss","feed_xml":"<html><body>challenge</body></html>"}`
	manualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manualRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/manual-submit", strings.NewReader(manualBody)))
	if manualRecorder.Code != http.StatusBadRequest {
		t.Fatalf("verification manual submit = %d %s", manualRecorder.Code, manualRecorder.Body.String())
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")
}

func TestVerificationManualSubmitRejectsWrongFeedURL(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if opts.VerifyFeedHost == nil {
			t.Fatal("expected host verification callback")
		}
		verification := opts.VerifyFeedHost([]feeds.VerificationRequest{{
			URL:     "https://www.cell.com/cell/current.rss",
			Target:  "cloudflare",
			Reason:  "challenge",
			Journal: "Cell",
		}})
		t.Fatalf("did not expect verification to finish: %#v", verification)
		return jobruntime.RunSummary{}, nil
	}
	startVerificationFlowFunc = func(_ config.Settings, _ *pendingVerification) error {
		return nil
	}

	handler := newTestHandler(t, testSettings(root))
	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run launch = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
	var runPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &runPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")

	manualBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://wrong.example/rss","feed_xml":"<rss><channel><title>Cell</title><item><title>Paper</title><link>https://example.com/paper</link></item></channel></rss>"}`
	manualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manualRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/manual-submit", strings.NewReader(manualBody)))
	if manualRecorder.Code != http.StatusNotFound {
		t.Fatalf("verification manual submit = %d %s", manualRecorder.Code, manualRecorder.Body.String())
	}
}

func TestVerificationManualSubmitRejectsWhenJobNotWaiting(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	handler := newTestHandler(t, testSettings(root))
	storeJob(jobInfo{
		ID:        "job-ready",
		JobType:   "sync",
		Status:    "completed",
		CreatedAt: time.Now().UTC(),
	})
	manualBody := `{"job_id":"job-ready","feed_url":"https://www.cell.com/cell/current.rss","feed_xml":"<rss><channel><title>Cell</title><item><title>Paper</title><link>https://example.com/paper</link></item></channel></rss>"}`
	manualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manualRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/manual-submit", strings.NewReader(manualBody)))
	if manualRecorder.Code != http.StatusBadRequest {
		t.Fatalf("verification manual submit = %d %s", manualRecorder.Code, manualRecorder.Body.String())
	}
	if !contains(manualRecorder.Body.String(), "Job is not waiting for manual verification.") {
		t.Fatalf("unexpected manual submit error: %s", manualRecorder.Body.String())
	}
}
