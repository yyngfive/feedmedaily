package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestApplyCleanupOperationsDeletesDuplicateAndMovesReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)

	canonicalID, _, err := store.UpsertPaper(Paper{
		SourceURL: "https://example.com/feed",
		Title:     "Canonical paper",
		URL:       "https://example.com/article",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	duplicateID, _, err := store.UpsertPaper(Paper{
		SourceURL: "https://example.com/feed",
		Title:     "Canonical paper",
		URL:       "https://example.com/article",
		DOI:       stringPointer("10.1000/wrong-doi"),
		Abstract:  stringPointer("Recovered abstract"),
		ReadAt:    timePointer(now.Add(time.Hour)),
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveClassification(canonicalID, Classification{
		Relevance: "direct", Confidence: 0.9, Reason: "fixture", TopicTags: []string{}, RecommendedAction: "read", Model: "fixture",
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertZoteroStatus(duplicateID, zoteroStateSaved, stringPointer("ABC"), nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`
		INSERT INTO feedback (paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at)
		VALUES (?, 'indirect', 'direct', 'duplicate feedback', 'open', 0, ?)
	`, duplicateID, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	result, err := store.ApplyCleanupOperations(context.Background(), []CleanupOperation{{
		Kind: CleanupOperationDeleteDuplicate, CandidatePaperID: duplicateID, CanonicalPaperID: canonicalID, Identity: CleanupIdentityURL,
	}}, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedDuplicates != 1 || result.Skipped != 0 {
		t.Fatalf("unexpected cleanup result: %#v", result)
	}
	deleted, err := store.PaperByID(duplicateID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != nil {
		t.Fatalf("duplicate paper still exists: %#v", deleted)
	}
	merged, err := store.PaperByID(canonicalID)
	if err != nil {
		t.Fatal(err)
	}
	if merged == nil || merged.Abstract == nil || *merged.Abstract != "Recovered abstract" {
		t.Fatalf("canonical paper did not retain richer content: %#v", merged)
	}
	if merged.DOI != nil {
		t.Fatalf("a URL duplicate's unrelated DOI must not be copied: %#v", merged.DOI)
	}
	feedback, err := store.ListFeedback()
	if err != nil {
		t.Fatal(err)
	}
	if len(feedback) != 1 || feedback[0].PaperID != canonicalID {
		t.Fatalf("feedback was not moved to canonical paper: %#v", feedback)
	}
	zotero, err := store.LatestZoteroStatus(canonicalID)
	if err != nil {
		t.Fatal(err)
	}
	if zotero == nil || !zotero.Saved || zotero.ItemKey == nil || *zotero.ItemKey != "ABC" {
		t.Fatalf("Zotero status was not moved to canonical paper: %#v", zotero)
	}
}

func TestApplyCleanupOperationsClearsDOIAndRepairsPaperKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	paperID, _, err := store.UpsertPaper(Paper{
		SourceURL: "https://example.com/feed", Title: "Repair me", URL: "https://Example.com/repair", DOI: stringPointer("10.1000/wrong"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.ApplyCleanupOperations(context.Background(), []CleanupOperation{{Kind: CleanupOperationClearDOI, CandidatePaperID: paperID}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.RepairedDOI != 1 {
		t.Fatalf("unexpected repair result: %#v", result)
	}
	paper, err := store.PaperByID(paperID)
	if err != nil {
		t.Fatal(err)
	}
	if paper == nil || paper.DOI != nil {
		t.Fatalf("DOI was not cleared: %#v", paper)
	}
	key, err := store.StoredPaperKey(paperID)
	if err != nil {
		t.Fatal(err)
	}
	if key != "url:https://example.com/repair" {
		t.Fatalf("paper key was not repaired: %q", key)
	}
}

func TestApplyCleanupOperationsRollsBackOnDOIKeyCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	if _, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "URL copy", URL: "https://example.com/collision"}, now); err != nil {
		t.Fatal(err)
	}
	doiID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "DOI copy", URL: "https://example.com/collision", DOI: stringPointer("10.1000/collision")}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ApplyCleanupOperations(context.Background(), []CleanupOperation{{Kind: CleanupOperationClearDOI, CandidatePaperID: doiID}}, now)
	if !errors.Is(err, ErrCleanupKeyCollision) {
		t.Fatalf("expected key collision, got %v", err)
	}
	paper, err := store.PaperByID(doiID)
	if err != nil {
		t.Fatal(err)
	}
	if paper == nil || paper.DOI == nil {
		t.Fatalf("failed transaction should retain DOI: %#v", paper)
	}
}

func TestCleanupReviewsPersistDecisionsAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	paperID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Review me", URL: "https://example.com/review"}, now)
	if err != nil {
		t.Fatal(err)
	}
	reviewID, err := store.UpsertCleanupReview(CleanupReviewDraft{
		GroupKey: "doi:review", CandidatePaperID: paperID, MatchType: CleanupReviewMatchDOIUnclear,
		Reason: "provider unavailable", SuggestedAction: CleanupDecisionKeep,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	review, err := store.CleanupReviewByID(reviewID)
	if err != nil {
		t.Fatal(err)
	}
	if review.Candidate.ID != paperID || review.State != CleanupReviewStatePending || review.Matched != nil {
		t.Fatalf("unexpected cleanup review: %#v", review)
	}
	if err := store.BackupTo(filepath.Join(t.TempDir(), "backup.sqlite")); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(t.TempDir(), "backup-2.sqlite")
	if err := store.BackupTo(backupPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup was not created: %v", err)
	}

	if _, err := store.ApplyCleanupReviewDecision(context.Background(), reviewID, "defer", now.Add(time.Hour)); err == nil {
		t.Fatal("unsupported defer decision should fail")
	}
	if _, err := store.ApplyCleanupReviewDecision(context.Background(), reviewID, CleanupDecisionKeep, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	kept, err := store.CleanupReviewByID(reviewID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.State != CleanupReviewStateKept || kept.Decision == nil || *kept.Decision != CleanupDecisionKeep {
		t.Fatalf("keep decision was not recorded: %#v", kept)
	}
	if _, err := store.ApplyCleanupReviewDecision(context.Background(), reviewID, CleanupDecisionKeep, now.Add(2*time.Hour)); !errors.Is(err, ErrCleanupReviewConflict) {
		t.Fatalf("expected resolved review conflict, got %v", err)
	}
}

func TestCleanupReviewCanDeleteMatchedPaperAndKeepCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)

	candidateID, _, err := store.UpsertPaper(Paper{
		SourceURL: "feed", Title: "Interstellar Ice Chemistry Yields Elusive Sulfinothioic Acid (HS(O)SH)", URL: "https://example.com/article",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	matchedID, _, err := store.UpsertPaper(Paper{
		SourceURL: "feed", Title: "Inside Back Cover: Interstellar Ice Chemistry Yields Elusive Sulfinothioic Acid (HS(O)SH)", URL: "https://example.com/inside-back-cover",
	}, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveClassification(matchedID, Classification{
		Relevance: "direct", Confidence: 0.9, Reason: "fixture", TopicTags: []string{}, RecommendedAction: "read", Model: "fixture",
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertZoteroStatus(matchedID, zoteroStateSaved, stringPointer("MATCHED"), nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`
		INSERT INTO feedback (paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at)
		VALUES (?, 'indirect', 'direct', 'keep the actual article', 'open', 0, ?)
	`, matchedID, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	reviewID, err := store.UpsertCleanupReview(CleanupReviewDraft{
		GroupKey:         "title:keep-candidate",
		CandidatePaperID: candidateID,
		MatchedPaperID:   matchedID,
		MatchType:        CleanupReviewMatchTitle,
		Reason:           "The possible match is an inside-back-cover entry.",
		SuggestedAction:  CleanupDecisionDelete,
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.ApplyCleanupReviewDecision(context.Background(), reviewID, CleanupDecisionDeleteMatch, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Deleted || result.PaperID != candidateID || result.DeletedPaperID != matchedID {
		t.Fatalf("unexpected delete-match result: %#v", result)
	}
	candidate, err := store.PaperByID(candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if candidate == nil || candidate.Title != "Interstellar Ice Chemistry Yields Elusive Sulfinothioic Acid (HS(O)SH)" {
		t.Fatalf("candidate article was not retained: %#v", candidate)
	}
	matched, err := store.PaperByID(matchedID)
	if err != nil {
		t.Fatal(err)
	}
	if matched != nil {
		t.Fatalf("possible match was not deleted: %#v", matched)
	}
	classification, err := store.LatestClassification(candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if classification != nil {
		t.Fatalf("retained candidate should remain unclassified: %#v", classification)
	}
	var deletedClassifications int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM classifications WHERE paper_id = ?`, matchedID).Scan(&deletedClassifications); err != nil {
		t.Fatal(err)
	}
	if deletedClassifications != 0 {
		t.Fatalf("deleted paper left classification rows: %d", deletedClassifications)
	}
	feedback, err := store.ListFeedback()
	if err != nil {
		t.Fatal(err)
	}
	if len(feedback) != 1 || feedback[0].PaperID != candidateID {
		t.Fatalf("feedback was not moved to retained candidate: %#v", feedback)
	}
	zotero, err := store.LatestZoteroStatus(candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if zotero == nil || !zotero.Saved || zotero.ItemKey == nil || *zotero.ItemKey != "MATCHED" {
		t.Fatalf("Zotero status was not moved to retained candidate: %#v", zotero)
	}
	review, err := store.CleanupReviewByID(reviewID)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != CleanupReviewStateKept || review.Decision == nil || *review.Decision != CleanupDecisionDeleteMatch {
		t.Fatalf("unexpected delete-match review state: %#v", review)
	}
}

func TestCleanupReviewCanClearMatchedDOIWithoutDeletingEitherPaper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	candidateDOI := stringPointer("10.1000/item-a")
	matchedDOI := stringPointer("10.1000/item-b")
	candidateID, _, err := store.UpsertPaper(Paper{
		SourceURL: "feed", Title: "Item A", URL: "https://example.com/item-a", DOI: candidateDOI,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	matchedID, _, err := store.UpsertPaper(Paper{
		SourceURL: "feed", Title: "Item B", URL: "https://example.com/item-b", DOI: matchedDOI,
	}, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	reviewID, err := store.UpsertCleanupReview(CleanupReviewDraft{
		GroupKey: "doi-conflict:clear-matched", CandidatePaperID: candidateID, MatchedPaperID: matchedID,
		MatchType: CleanupReviewMatchDOIConflict, Reason: "conflicting DOI metadata", SuggestedAction: CleanupDecisionKeep,
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.ApplyCleanupReviewDecision(context.Background(), reviewID, CleanupDecisionClearMatchDOI, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted || result.DeletedPaperID != 0 || !result.DOICleared || result.PaperID != candidateID {
		t.Fatalf("unexpected clear matched DOI result: %#v", result)
	}
	candidate, err := store.PaperByID(candidateID)
	if err != nil {
		t.Fatal(err)
	}
	matched, err := store.PaperByID(matchedID)
	if err != nil {
		t.Fatal(err)
	}
	if candidate == nil || candidate.DOI == nil || matched == nil || matched.DOI != nil {
		t.Fatalf("unexpected papers after clearing matched DOI: candidate=%#v matched=%#v", candidate, matched)
	}
	review, err := store.CleanupReviewByID(reviewID)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != CleanupReviewStateDOIClear || review.Decision == nil || *review.Decision != CleanupDecisionClearMatchDOI {
		t.Fatalf("unexpected clear matched DOI review state: %#v", review)
	}
}

func TestOpenOrCreateMigratesDeferredCleanupReviewsToPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	paperID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Legacy review", URL: "https://example.com/legacy-review"}, now)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	reviewID, err := store.UpsertCleanupReview(CleanupReviewDraft{
		GroupKey: "legacy:review", CandidatePaperID: paperID, MatchType: CleanupReviewMatchDOIUnclear,
		Reason: "legacy", SuggestedAction: CleanupDecisionKeep,
	}, now)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE cleanup_reviews SET state = ? WHERE id = ?`, CleanupReviewStateDeferred, reviewID); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	review, err := reopened.CleanupReviewByID(reviewID)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != CleanupReviewStatePending {
		t.Fatalf("legacy deferred review was not migrated: %#v", review)
	}
}

func TestOpenOrCreateMigratesLegacyDOIConflictTitleReviewBackToTitleDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	candidateID, _, err := store.UpsertPaper(Paper{
		SourceURL: "feed", Title: "Item A", URL: "https://example.com/item-a", DOI: stringPointer("10.1000/item-a"),
	}, now)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	matchedID, _, err := store.UpsertPaper(Paper{
		SourceURL: "feed", Title: "Item B", URL: "https://example.com/item-b", DOI: stringPointer("10.1000/item-b"),
	}, now.Add(time.Hour))
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	reviewID, err := store.UpsertCleanupReview(CleanupReviewDraft{
		GroupKey: "title:legacy-conflict", CandidatePaperID: candidateID, MatchedPaperID: matchedID,
		MatchType: CleanupReviewMatchDOIConflict, Reason: "legacy DOI conflict review", SuggestedAction: CleanupDecisionKeep,
	}, now)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	review, err := reopened.CleanupReviewByID(reviewID)
	if err != nil {
		t.Fatal(err)
	}
	if review.MatchType != CleanupReviewMatchTitle || review.SuggestedAction != CleanupDecisionDelete || review.State != CleanupReviewStatePending {
		t.Fatalf("legacy DOI conflict title review was not migrated back to title duplicate: %#v", review)
	}
}

func TestApplyCleanupOperationsClosesReviewsReferencingDeletedDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)

	canonicalID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Canonical", URL: "https://example.com/canonical"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveClassification(canonicalID, Classification{
		Relevance: "direct", Confidence: 0.9, Reason: "fixture", TopicTags: []string{}, RecommendedAction: "read", Model: "fixture",
	}, now); err != nil {
		t.Fatal(err)
	}
	// A DOI gives the duplicate a distinct paper_key so it is stored as its own row.
	duplicateID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Canonical", URL: "https://example.com/canonical", DOI: stringPointer("10.1000/duplicate")}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	partnerID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Shared Title Variant", URL: "https://example.com/partner"}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	reviewID, err := store.UpsertCleanupReview(CleanupReviewDraft{
		GroupKey: "title:partner-cascade", CandidatePaperID: partnerID, MatchedPaperID: duplicateID,
		MatchType: CleanupReviewMatchTitle, Reason: "title match", SuggestedAction: CleanupDecisionDelete,
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.ApplyCleanupOperations(context.Background(), []CleanupOperation{{
		Kind: CleanupOperationDeleteDuplicate, CandidatePaperID: duplicateID, CanonicalPaperID: canonicalID, Identity: CleanupIdentityURL,
	}}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	review, err := store.CleanupReviewByID(reviewID)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != CleanupReviewStateDeleted || review.Decision == nil || *review.Decision != CleanupDecisionDelete {
		t.Fatalf("pending review referencing the deleted duplicate as matched was not closed: %#v", review)
	}
	partner, err := store.PaperByID(partnerID)
	if err != nil {
		t.Fatal(err)
	}
	if partner == nil {
		t.Fatal("review candidate must survive the cascade")
	}
}

func TestApplyCleanupReviewDecisionClosesReviewsReferencingDeletedPaper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)

	retainedID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Retained article", URL: "https://example.com/retained"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveClassification(retainedID, Classification{
		Relevance: "direct", Confidence: 0.9, Reason: "fixture", TopicTags: []string{}, RecommendedAction: "read", Model: "fixture",
	}, now); err != nil {
		t.Fatal(err)
	}
	deletedID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Deleted article", URL: "https://example.com/deleted"}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	observerID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Deleted article", URL: "https://example.com/observer"}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	// observer's review points at the paper another decision is about to delete.
	observerReviewID, err := store.UpsertCleanupReview(CleanupReviewDraft{
		GroupKey: "title:observer", CandidatePaperID: observerID, MatchedPaperID: deletedID,
		MatchType: CleanupReviewMatchTitle, Reason: "title match", SuggestedAction: CleanupDecisionDelete,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	deletedReviewID, err := store.UpsertCleanupReview(CleanupReviewDraft{
		GroupKey: "title:deleted", CandidatePaperID: deletedID, MatchedPaperID: retainedID,
		MatchType: CleanupReviewMatchTitle, Reason: "title match", SuggestedAction: CleanupDecisionDelete,
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.ApplyCleanupReviewDecision(context.Background(), deletedReviewID, CleanupDecisionDelete, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	observerReview, err := store.CleanupReviewByID(observerReviewID)
	if err != nil {
		t.Fatal(err)
	}
	if observerReview.State != CleanupReviewStateDeleted || observerReview.Decision == nil || *observerReview.Decision != CleanupDecisionDelete {
		t.Fatalf("pending review whose matched paper was deleted by another decision was not closed: %#v", observerReview)
	}
	// The decided review's candidate paper is gone, so the listing JOIN hides
	// it; assert its recorded state directly.
	var deletedState string
	var deletedDecision *string
	if err := store.db.QueryRow(`SELECT state, decision FROM cleanup_reviews WHERE id = ?`, deletedReviewID).Scan(&deletedState, &deletedDecision); err != nil {
		t.Fatal(err)
	}
	if deletedState != CleanupReviewStateDeleted || deletedDecision == nil || *deletedDecision != CleanupDecisionDelete {
		t.Fatalf("decided review did not keep its own decision record: state=%q decision=%v", deletedState, deletedDecision)
	}
	observer, err := store.PaperByID(observerID)
	if err != nil {
		t.Fatal(err)
	}
	if observer == nil {
		t.Fatal("observer paper must survive the cascade")
	}
}

func TestOpenOrCreateClosesCleanupReviewsWithDeletedMatchedPapers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	candidateID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Stuck review", URL: "https://example.com/stuck"}, now)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	matchedID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Vanished match", URL: "https://example.com/vanished"}, now.Add(time.Minute))
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	reviewID, err := store.UpsertCleanupReview(CleanupReviewDraft{
		GroupKey: "title:stuck", CandidatePaperID: candidateID, MatchedPaperID: matchedID,
		MatchType: CleanupReviewMatchTitle, Reason: "title match", SuggestedAction: CleanupDecisionDelete,
	}, now)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	// Simulate a legacy database where the matched paper was deleted without
	// closing reviews that referenced it.
	if _, err := store.db.Exec(`DELETE FROM papers WHERE id = ?`, matchedID); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	review, err := reopened.CleanupReviewByID(reviewID)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != CleanupReviewStateDeleted || review.Decision == nil || *review.Decision != CleanupDecisionDelete || review.DecidedAt == nil {
		t.Fatalf("dead matched reference was not repaired at startup: %#v", review)
	}
}

func TestOpenOrCreateMigratesLegacyTitleReviewKeysToPairKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	firstID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Mirrored pair", URL: "https://example.com/first"}, now)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	secondID, _, err := store.UpsertPaper(Paper{SourceURL: "feed", Title: "Mirrored pair", URL: "https://example.com/second"}, now.Add(time.Minute))
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	// Mirrored legacy rows: each direction created its own candidate-keyed row.
	for _, legacy := range []struct {
		groupKey  string
		candidate int64
		matched   int64
	}{
		{groupKey: "title:" + strconv.FormatInt(firstID, 10), candidate: firstID, matched: secondID},
		{groupKey: "title:" + strconv.FormatInt(secondID, 10), candidate: secondID, matched: firstID},
	} {
		if _, err := store.db.Exec(`
			INSERT INTO cleanup_reviews (group_key, candidate_paper_id, matched_paper_id, match_type, reason, suggested_action, state, created_at)
			VALUES (?, ?, ?, 'title_duplicate', 'legacy mirror', 'delete', 'pending', ?)
		`, legacy.groupKey, legacy.candidate, legacy.matched, now.Format(time.RFC3339Nano)); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	pairKey := CleanupTitleReviewGroupKey(firstID, secondID)
	if pairKey == "title:"+strconv.FormatInt(firstID, 10) {
		t.Fatalf("pair key must be direction independent: %q", pairKey)
	}
	assertSinglePendingPairReview(t, reopened, pairKey)

	// Reopening must be a no-op once keys are migrated.
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedAgain, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedAgain.Close()
	assertSinglePendingPairReview(t, reopenedAgain, pairKey)
}

func assertSinglePendingPairReview(t *testing.T, store *Store, pairKey string) {
	t.Helper()
	var groupKey string
	var state string
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM cleanup_reviews`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("mirrored legacy reviews did not collapse into one row: count=%d", count)
	}
	if err := store.db.QueryRow(`SELECT group_key, state FROM cleanup_reviews LIMIT 1`).Scan(&groupKey, &state); err != nil {
		t.Fatal(err)
	}
	if groupKey != pairKey || state != CleanupReviewStatePending {
		t.Fatalf("unexpected migrated review: key=%q state=%q", groupKey, state)
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}
