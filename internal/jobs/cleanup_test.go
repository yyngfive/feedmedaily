package jobs

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestBuildExactDuplicateOperationsKeepsClassifiedCanonical(t *testing.T) {
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	papers := []store.CleanupPaper{
		{Paper: store.Paper{ID: 1, Title: "Canonical", URL: "https://example.com/article", DOI: stringPointerForCleanup("10.1000/shared"), FirstSeenAt: now}, Classified: true},
		{Paper: store.Paper{ID: 2, Title: "Canonical", URL: "https://example.com/article", FirstSeenAt: now.Add(time.Hour)}},
		{Paper: store.Paper{ID: 3, Title: "Canonical", URL: "https://example.com/other", DOI: stringPointerForCleanup("DOI:10.1000/shared"), FirstSeenAt: now.Add(2 * time.Hour)}},
		{Paper: store.Paper{ID: 4, Title: "Independent", URL: "https://example.com/independent", FirstSeenAt: now}, Classified: false},
	}

	operations, deletedIDs, groups := buildExactDuplicateOperations(papers)
	if groups != 1 || len(operations) != 2 {
		t.Fatalf("unexpected exact duplicate plan: groups=%d operations=%#v", groups, operations)
	}
	if len(deletedIDs) != 2 {
		t.Fatalf("unexpected deleted ids: %#v", deletedIDs)
	}
	for _, operation := range operations {
		if operation.CanonicalPaperID != 1 || operation.Kind != store.CleanupOperationDeleteDuplicate {
			t.Fatalf("unexpected operation: %#v", operation)
		}
		if operation.CandidatePaperID == 2 && operation.Identity != store.CleanupIdentityURL {
			t.Fatalf("URL duplicate should use URL identity: %#v", operation)
		}
		if operation.CandidatePaperID == 3 && operation.Identity != store.CleanupIdentityDOI {
			t.Fatalf("DOI duplicate should use DOI identity: %#v", operation)
		}
	}
}

func TestFindTitleDuplicateRequiresCompatibleDateButStillReturnsReviewCandidate(t *testing.T) {
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	candidate := store.CleanupPaper{Paper: store.Paper{ID: 10, Title: "A precise title", URL: "https://example.com/candidate", PublishedDate: stringPointerForCleanup("2026-06-11"), FirstSeenAt: now}}
	papers := []store.CleanupPaper{
		candidate,
		{Paper: store.Paper{ID: 11, Title: "A precise title", URL: "https://example.com/match", PublishedDate: stringPointerForCleanup("2026-06-27"), FirstSeenAt: now.Add(time.Hour)}, Classified: true},
		{Paper: store.Paper{ID: 12, Title: "A precise title", URL: "https://example.com/wrong-date", PublishedDate: stringPointerForCleanup("2025-06-01"), FirstSeenAt: now.Add(-time.Hour)}, Classified: true},
	}
	matched := findTitleDuplicate(candidate, papers, map[int64]struct{}{})
	if matched == nil || matched.Paper.ID != 11 {
		t.Fatalf("unexpected title duplicate: %#v", matched)
	}

	noDateCandidate := candidate
	noDateCandidate.Paper.ID = 20
	noDateCandidate.Paper.PublishedDate = nil
	noDateMatch := findTitleDuplicate(noDateCandidate, papers[:2], map[int64]struct{}{})
	if noDateMatch == nil || noDateMatch.Paper.ID != 11 {
		t.Fatalf("title-only match should be sent to review: %#v", noDateMatch)
	}
}

func TestCleanupKeepsTitleDuplicateReviewWhenTwoDOIsArePresent(t *testing.T) {
	root := t.TempDir()
	settings := testJobSettings(root)
	writeTestProfile(t, settings)
	sqliteStore, err := store.OpenOrCreate(filepath.Join(root, "data", "literature.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	canonicalID, _, err := sqliteStore.UpsertPaper(store.Paper{
		SourceURL: "feed", Title: "Inside Back Cover: Interstellar Ice Chemistry Yields Elusive Sulfinothioic Acid (HS(O)SH)", URL: "https://example.com/inside-back-cover", DOI: stringPointerForCleanup("10.1002/anie.2026-m0808105300"),
	}, now)
	if err != nil {
		sqliteStore.Close()
		t.Fatal(err)
	}
	candidateID, _, err := sqliteStore.UpsertPaper(store.Paper{
		SourceURL: "feed", Title: "Interstellar Ice Chemistry Yields Elusive Sulfinothioic Acid (HS(O)SH)", URL: "https://example.com/article", DOI: stringPointerForCleanup("10.1002/anie.8731327"),
	}, now.Add(time.Hour))
	if err != nil {
		sqliteStore.Close()
		t.Fatal(err)
	}
	if err := sqliteStore.SaveClassification(canonicalID, store.Classification{
		Relevance: "direct", Confidence: 0.9, Reason: "fixture", TopicTags: []string{}, RecommendedAction: "read", Model: "fixture",
	}, now); err != nil {
		sqliteStore.Close()
		t.Fatal(err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := CleanupUnclassifiedContext(settings, context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["reclassified"] != 0 || result["needs_review"] != 1 || result["awaiting_review"] != true {
		t.Fatalf("cleanup should pause on a title duplicate: %#v", result)
	}
	reopened, err := store.Open(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	candidate, err := reopened.PaperByID(candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if candidate == nil || candidate.DOI == nil {
		t.Fatalf("DOI conflict candidate was deleted or changed: %#v", candidate)
	}
	reviews, err := reopened.ListCleanupReviews(store.CleanupReviewStatePending)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].MatchType != store.CleanupReviewMatchTitle || reviews[0].Candidate.ID != candidateID || reviews[0].Matched == nil || reviews[0].Matched.ID != canonicalID {
		t.Fatalf("unexpected title duplicate review: %#v", reviews)
	}
}

func TestSeparateExactDOIConflictOperationsCreatesReviewInsteadOfDelete(t *testing.T) {
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	papers := []store.CleanupPaper{
		{Paper: store.Paper{ID: 1, Title: "Canonical title", DOI: stringPointerForCleanup("10.1000/shared"), FirstSeenAt: now}, Classified: true},
		{Paper: store.Paper{ID: 2, Title: "Different title", DOI: stringPointerForCleanup("10.1000/shared"), FirstSeenAt: now.Add(time.Hour)}},
	}
	operations := []store.CleanupOperation{{
		Kind: store.CleanupOperationDeleteDuplicate, CandidatePaperID: 2, CanonicalPaperID: 1,
		Identity: store.CleanupIdentityDOI,
	}}
	deletedIDs := map[int64]struct{}{2: {}}
	keptOperations, drafts, conflicts := separateDOIConflictOperations(operations, deletedIDs, papers, map[int64]string{})
	if len(keptOperations) != 0 || len(drafts) != 1 || drafts[0].MatchType != store.CleanupReviewMatchDOIConflict || len(conflicts) != 1 {
		t.Fatalf("unexpected DOI conflict split: operations=%#v drafts=%#v conflicts=%#v", keptOperations, drafts, conflicts)
	}
	if len(operations) != 1 || len(deletedIDs) != 0 {
		t.Fatalf("unexpected DOI conflict source state: operations=%#v deleted=%#v", operations, deletedIDs)
	}
}

func TestCleanupPausesBulkClassificationWhileReviewsArePending(t *testing.T) {
	root := t.TempDir()
	settings := testJobSettings(root)
	writeTestProfile(t, settings)
	sqliteStore, err := store.OpenOrCreate(filepath.Join(root, "data", "literature.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	canonicalID, _, err := sqliteStore.UpsertPaper(store.Paper{
		SourceURL: "feed", Title: "A title needing review", URL: "https://example.com/canonical",
	}, now)
	if err != nil {
		sqliteStore.Close()
		t.Fatal(err)
	}
	candidateID, _, err := sqliteStore.UpsertPaper(store.Paper{
		SourceURL: "feed", Title: "A title needing review", URL: "https://example.com/candidate",
	}, now.Add(time.Minute))
	if err != nil {
		sqliteStore.Close()
		t.Fatal(err)
	}
	independentID, _, err := sqliteStore.UpsertPaper(store.Paper{
		SourceURL: "feed", Title: "An independent paper", URL: "https://example.com/independent",
	}, now.Add(2*time.Minute))
	if err != nil {
		sqliteStore.Close()
		t.Fatal(err)
	}
	if err := sqliteStore.SaveClassification(canonicalID, store.Classification{
		Relevance: "direct", Confidence: 0.9, Reason: "fixture", TopicTags: []string{}, RecommendedAction: "read", Model: "fixture",
	}, now); err != nil {
		sqliteStore.Close()
		t.Fatal(err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := CleanupUnclassifiedContext(settings, context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["reclassified"] != 0 || result["needs_review"] != 1 || result["awaiting_review"] != true {
		t.Fatalf("cleanup should pause before bulk classification: %#v", result)
	}
	reopened, err := store.Open(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	classification, err := reopened.LatestClassification(independentID)
	if err != nil {
		t.Fatal(err)
	}
	if classification != nil {
		t.Fatalf("independent paper was classified before review: %#v", classification)
	}
	reviews, err := reopened.ListCleanupReviews(store.CleanupReviewStatePending)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].Candidate.ID != candidateID {
		t.Fatalf("unexpected pending reviews: %#v", reviews)
	}
}

func TestResolveCleanupReviewLeavesKeepAndClearDOIUnclassified(t *testing.T) {
	tests := []struct {
		name     string
		decision string
		doi      *string
		wantDOI  bool
	}{
		{name: "keep", decision: store.CleanupDecisionKeep},
		{name: "clear doi", decision: store.CleanupDecisionClearDOI, doi: stringPointerForCleanup("10.1000/unverified"), wantDOI: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			settings := testJobSettings(root)
			settings.ClassifierAPIKey = ""
			writeTestProfile(t, settings)
			sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
			paperID, _, err := sqliteStore.UpsertPaper(store.Paper{
				SourceURL: "feed",
				Title:     "Review decision should wait",
				URL:       "https://example.com/review-decision",
				DOI:       tt.doi,
			}, now)
			if err != nil {
				sqliteStore.Close()
				t.Fatal(err)
			}
			reviewID, err := sqliteStore.UpsertCleanupReview(store.CleanupReviewDraft{
				GroupKey:         "review-decision:" + tt.name,
				CandidatePaperID: paperID,
				MatchType:        store.CleanupReviewMatchDOIUnclear,
				Reason:           "needs a human decision",
				SuggestedAction:  store.CleanupDecisionKeep,
			}, now)
			if err != nil {
				sqliteStore.Close()
				t.Fatal(err)
			}
			if err := sqliteStore.Close(); err != nil {
				t.Fatal(err)
			}

			result, err := ResolveCleanupReviewContext(settings, reviewID, tt.decision, context.Background(), nil, nil)
			if err != nil {
				t.Fatalf("review decision should not invoke the classifier: %v", err)
			}
			if result["reclassified"] != 0 {
				t.Fatalf("review decision reclassified paper immediately: %#v", result)
			}
			if result["queued_for_classification"] != true {
				t.Fatalf("review decision did not leave the paper queued for the next cleanup batch: %#v", result)
			}

			reopened, err := store.Open(settings.DatabasePath)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			classification, err := reopened.LatestClassification(paperID)
			if err != nil {
				t.Fatal(err)
			}
			if classification != nil {
				t.Fatalf("paper was classified before the next cleanup run: %#v", classification)
			}
			paper, err := reopened.PaperByID(paperID)
			if err != nil {
				t.Fatal(err)
			}
			if paper == nil || (paper.DOI != nil) != tt.wantDOI {
				t.Fatalf("unexpected DOI after %s decision: %#v", tt.decision, paper)
			}
			review, err := reopened.CleanupReviewByID(reviewID)
			if err != nil {
				t.Fatal(err)
			}
			wantState := store.CleanupReviewStateKept
			if tt.decision == store.CleanupDecisionClearDOI {
				wantState = store.CleanupReviewStateDOIClear
			}
			if review.State != wantState {
				t.Fatalf("review state = %q, want %q", review.State, wantState)
			}
		})
	}
}

func stringPointerForCleanup(value string) *string {
	return &value
}
