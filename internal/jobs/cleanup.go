package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	"github.com/yyngfive/scirssagent/internal/logging"
	"github.com/yyngfive/scirssagent/internal/metadata"
	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

// CleanupUnclassifiedContext 扫描全部未分类文章，先处理确定性重复和明确
// DOI 错配，再把没有进入人工队列的文章交给现有分类流程。只要扫描后
// 仍有待人工复核项，就暂停批量分类；用户处理完复核项后再次运行清理，
// 才会继续分类剩余文章。
func CleanupUnclassifiedContext(settings config.Settings, ctx context.Context, progress ProgressFunc, usage *llmusage.Collector) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if current, err := profile.ReadCurrent(settings.ProfilePath); err != nil {
		return nil, err
	} else if current == nil {
		return nil, fmt.Errorf("No classification profile exists yet.")
	}

	logging.SetDefaultDir(settings.LogsDir)
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return nil, err
	}
	defer sqliteStore.Close()

	papers, err := sqliteStore.ListPapersForCleanup()
	if err != nil {
		return nil, err
	}
	unclassified := make([]store.CleanupPaper, 0)
	for _, item := range papers {
		if !item.Classified {
			unclassified = append(unclassified, item)
		}
	}
	result := map[string]any{
		"scope":              "unclassified-cleanup",
		"scanned":            len(unclassified),
		"duplicate_groups":   0,
		"deleted_duplicates": 0,
		"repaired_doi":       0,
		"reclassified":       0,
		"needs_review":       0,
	}
	if len(unclassified) == 0 {
		if pending, err := sqliteStore.CountCleanupReviews(store.CleanupReviewStatePending); err == nil {
			result["pending_review_count"] = pending
			if pending > 0 {
				result["needs_review"] = pending
				result["awaiting_review"] = true
			}
		}
		return result, nil
	}

	reviewStates, err := sqliteStore.CleanupReviewStates()
	if err != nil {
		return result, err
	}
	operations, deletedIDs, duplicateGroups := buildExactDuplicateOperations(papers)
	result["duplicate_groups"] = duplicateGroups
	operations, doiConflictDrafts, doiConflictIDs := separateDOIConflictOperations(operations, deletedIDs, papers, reviewStates)
	reviewDrafts := append([]store.CleanupReviewDraft{}, doiConflictDrafts...)

	remaining := make([]store.CleanupPaper, 0, len(unclassified))
	for _, item := range unclassified {
		if _, deleted := deletedIDs[item.Paper.ID]; deleted {
			continue
		}
		remaining = append(remaining, item)
	}

	classifyIDs := make([]int64, 0, len(remaining))
	for index, item := range remaining {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		current := index + 1
		EmitProgress(progress, PercentProgress(
			"pipeline.cleanup.scanning",
			"cleanup",
			current,
			len(remaining),
			fmt.Sprintf("Checking unclassified papers %d/%d.", current, len(remaining)),
		))

		// pending 是用户尚未完成的人工决定，不应在下一次全库扫描中
		// 被偷偷送给分类器。
		reviewState := reviewStates[item.Paper.ID]
		if reviewState == store.CleanupReviewStatePending {
			continue
		}
		if _, doiConflict := doiConflictIDs[item.Paper.ID]; doiConflict && reviewState != store.CleanupReviewStateKept && reviewState != store.CleanupReviewStateDOIClear {
			continue
		}

		// A resolved keep/clear-DOI decision is an explicit user override. Honor
		// it on a retry (for example after a classifier failure) instead of
		// reopening the same title review forever.
		if reviewState != store.CleanupReviewStateKept && reviewState != store.CleanupReviewStateDOIClear {
			matched := findTitleDuplicate(item, papers, deletedIDs)
			if matched != nil {
				reason := "The normalized title matches another paper, but no exact URL or DOI match was found. Confirm whether this is a duplicate."
				if stringValue(item.Paper.DOI) != "" {
					reason += " The DOI was left unchanged until this title match is reviewed."
				}
				reviewDrafts = append(reviewDrafts, store.CleanupReviewDraft{
					GroupKey:         store.CleanupTitleReviewGroupKey(item.Paper.ID, matched.Paper.ID),
					CandidatePaperID: item.Paper.ID,
					MatchedPaperID:   matched.Paper.ID,
					MatchType:        store.CleanupReviewMatchTitle,
					Reason:           reason,
					SuggestedAction:  store.CleanupDecisionDelete,
				})
				continue
			}

			if item.Paper.DOI != nil && strings.TrimSpace(*item.Paper.DOI) != "" {
				verification := metadata.ValidateDOI(ctx, item.Paper)
				switch verification.Verdict {
				case metadata.DOIVerdictMismatch:
					operations = append(operations, store.CleanupOperation{
						Kind:             store.CleanupOperationClearDOI,
						CandidatePaperID: item.Paper.ID,
						Reason:           verification.Detail,
					})
				case metadata.DOIVerdictUncertain:
					reviewDrafts = append(reviewDrafts, store.CleanupReviewDraft{
						GroupKey:         "doi:" + strconv.FormatInt(item.Paper.ID, 10),
						CandidatePaperID: item.Paper.ID,
						MatchType:        store.CleanupReviewMatchDOIUnclear,
						Reason:           "The DOI could not be confirmed against the stored title and publication date. No automatic change was made. " + strings.TrimSpace(verification.Detail),
						SuggestedAction:  store.CleanupDecisionKeep,
					})
					continue
				}
			}
		}
		classifyIDs = append(classifyIDs, item.Paper.ID)
	}

	result["needs_review"] = len(reviewDrafts)
	if len(operations) == 0 && len(reviewDrafts) == 0 && len(classifyIDs) == 0 {
		if pending, err := sqliteStore.CountCleanupReviews(store.CleanupReviewStatePending); err == nil {
			result["pending_review_count"] = pending
			if pending > 0 {
				result["needs_review"] = pending
				result["awaiting_review"] = true
			}
		}
		return result, nil
	}

	now := time.Now().UTC()
	backupPath := cleanupBackupPath(settings, now)
	if err := sqliteStore.BackupTo(backupPath); err != nil {
		return result, err
	}
	result["backup_path"] = backupPath
	pruneCleanupBackups(cleanupBackupDir(settings))

	for _, draft := range reviewDrafts {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if _, err := sqliteStore.UpsertCleanupReview(draft, now); err != nil {
			return result, err
		}
	}
	applyResult, err := sqliteStore.ApplyCleanupOperations(ctx, operations, now)
	result["deleted_duplicates"] = applyResult.DeletedDuplicates
	result["repaired_doi"] = applyResult.RepairedDOI
	if err != nil {
		return result, err
	}

	pendingReviews, err := sqliteStore.CountCleanupReviews(store.CleanupReviewStatePending)
	if err != nil {
		return result, err
	}
	// The pending count is authoritative: a draft may have refreshed an already
	// resolved pair review instead of creating new work, so report what is
	// actually waiting rather than how many drafts the scan produced.
	result["pending_review_count"] = pendingReviews
	result["needs_review"] = pendingReviews
	if pendingReviews > 0 {
		result["awaiting_review"] = true
		if operationsCount := applyResult.DeletedDuplicates + applyResult.RepairedDOI; operationsCount > 0 {
			reportCount, err := RebuildLatestReport(settings, progress)
			if err != nil {
				return result, err
			}
			result["report_papers"] = reportCount
		}
		return result, nil
	}

	if len(classifyIDs) > 0 {
		classified, err := ReclassifyPaperIDsContext(settings, classifyIDs, ctx, progress, usage)
		result["reclassified"] = classified
		if err != nil {
			return result, err
		}
	}
	if len(operations) > 0 || len(classifyIDs) > 0 {
		reportCount, err := RebuildLatestReport(settings, progress)
		if err != nil {
			return result, err
		}
		result["report_papers"] = reportCount
	}
	return result, nil
}

// ResolveCleanupReviewContext applies one explicit human decision.
// Paper mutation is transactional. Keep/clear-DOI decisions deliberately
// leave the article unclassified so it joins the next cleanup batch.
func ResolveCleanupReviewContext(settings config.Settings, reviewID int64, decision string, ctx context.Context, progress ProgressFunc, _ *llmusage.Collector) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return nil, err
	}
	defer sqliteStore.Close()
	review, err := sqliteStore.CleanupReviewByID(reviewID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	backupPath := cleanupBackupPath(settings, now)
	if err := sqliteStore.BackupTo(backupPath); err != nil {
		return nil, err
	}
	pruneCleanupBackups(cleanupBackupDir(settings))
	EmitProgress(progress, StepProgress(
		"pipeline.cleanup.applying",
		"cleanup-review",
		1,
		2,
		"Applying the selected cleanup review decision.",
	))
	decisionResult, err := sqliteStore.ApplyCleanupReviewDecision(ctx, reviewID, decision, now)
	result := map[string]any{
		"review_id":        decisionResult.ReviewID,
		"paper_id":         decisionResult.PaperID,
		"decision":         decisionResult.Decision,
		"deleted":          decisionResult.Deleted,
		"deleted_paper_id": decisionResult.DeletedPaperID,
		"doi_cleared":      decisionResult.DOICleared,
		"backup_path":      backupPath,
		"review_type":      review.MatchType,
		"reclassified":     0,
	}
	if err != nil {
		return result, err
	}
	if decision == store.CleanupDecisionKeep || decision == store.CleanupDecisionDeleteMatch || decision == store.CleanupDecisionClearDOI || decision == store.CleanupDecisionClearMatchDOI {
		result["queued_for_classification"] = true
	}
	reportCount, err := RebuildLatestReport(settings, progress)
	if err != nil {
		return result, err
	}
	result["report_papers"] = reportCount
	return result, nil
}

type cleanupUnionFind struct {
	parent map[int64]int64
}

func newCleanupUnionFind(ids []int64) *cleanupUnionFind {
	parent := make(map[int64]int64, len(ids))
	for _, id := range ids {
		parent[id] = id
	}
	return &cleanupUnionFind{parent: parent}
}

func (u *cleanupUnionFind) find(id int64) int64 {
	parent, ok := u.parent[id]
	if !ok || parent == id {
		return id
	}
	root := u.find(parent)
	u.parent[id] = root
	return root
}

func (u *cleanupUnionFind) union(left int64, right int64) {
	leftRoot, rightRoot := u.find(left), u.find(right)
	if leftRoot != rightRoot {
		u.parent[rightRoot] = leftRoot
	}
}

func buildExactDuplicateOperations(papers []store.CleanupPaper) ([]store.CleanupOperation, map[int64]struct{}, int) {
	ids := make([]int64, 0, len(papers))
	for _, item := range papers {
		ids = append(ids, item.Paper.ID)
	}
	unionFind := newCleanupUnionFind(ids)
	for _, grouped := range exactKeyGroups(papers) {
		if len(grouped) < 2 {
			continue
		}
		first := grouped[0]
		for _, item := range grouped[1:] {
			unionFind.union(first.Paper.ID, item.Paper.ID)
		}
	}
	components := map[int64][]store.CleanupPaper{}
	for _, item := range papers {
		root := unionFind.find(item.Paper.ID)
		components[root] = append(components[root], item)
	}

	componentList := make([][]store.CleanupPaper, 0, len(components))
	for _, component := range components {
		componentList = append(componentList, component)
	}
	sort.Slice(componentList, func(i, j int) bool {
		return smallestPaperID(componentList[i]) < smallestPaperID(componentList[j])
	})
	operations := []store.CleanupOperation{}
	deletedIDs := map[int64]struct{}{}
	duplicateGroups := 0
	for _, component := range componentList {
		unclassified := make([]store.CleanupPaper, 0)
		for _, item := range component {
			if !item.Classified {
				unclassified = append(unclassified, item)
			}
		}
		if len(component) < 2 || len(unclassified) == 0 {
			continue
		}
		duplicateGroups++
		sort.SliceStable(component, func(i, j int) bool {
			if component[i].Classified != component[j].Classified {
				return component[i].Classified
			}
			if !component[i].Paper.FirstSeenAt.Equal(component[j].Paper.FirstSeenAt) {
				return component[i].Paper.FirstSeenAt.Before(component[j].Paper.FirstSeenAt)
			}
			return component[i].Paper.ID < component[j].Paper.ID
		})
		canonical := component[0]
		for _, item := range component {
			if item.Classified {
				canonical = item
				break
			}
		}
		for _, item := range unclassified {
			if item.Paper.ID == canonical.Paper.ID {
				continue
			}
			identity := store.CleanupIdentityURL
			if normalizedDOI(item.Paper.DOI) != "" && normalizedDOI(item.Paper.DOI) == normalizedDOI(canonical.Paper.DOI) {
				identity = store.CleanupIdentityDOI
			}
			operations = append(operations, store.CleanupOperation{
				Kind:             store.CleanupOperationDeleteDuplicate,
				CandidatePaperID: item.Paper.ID,
				CanonicalPaperID: canonical.Paper.ID,
				Identity:         identity,
				Reason:           "The unclassified paper shares an exact normalized URL or DOI with another stored paper.",
			})
			deletedIDs[item.Paper.ID] = struct{}{}
		}
	}
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].CandidatePaperID != operations[j].CandidatePaperID {
			return operations[i].CandidatePaperID < operations[j].CandidatePaperID
		}
		return operations[i].CanonicalPaperID < operations[j].CanonicalPaperID
	})
	return operations, deletedIDs, duplicateGroups
}

// separateDOIConflictOperations keeps an exact duplicate relation in the
// review queue when both entries have DOI values but their stored titles
// differ. Exact URL/DOI identity is not enough evidence to delete either
// article in that case; the user must decide which DOI, if any, should be
// removed. Resolved keep/DOI decisions allow the retained candidate to
// proceed to the next classification batch without reopening the conflict.
func separateDOIConflictOperations(
	operations []store.CleanupOperation,
	deletedIDs map[int64]struct{},
	papers []store.CleanupPaper,
	reviewStates map[int64]string,
) ([]store.CleanupOperation, []store.CleanupReviewDraft, map[int64]struct{}) {
	paperByID := make(map[int64]store.CleanupPaper, len(papers))
	for _, item := range papers {
		paperByID[item.Paper.ID] = item
	}
	drafts := []store.CleanupReviewDraft{}
	conflictIDs := map[int64]struct{}{}
	keptOperations := make([]store.CleanupOperation, 0, len(operations))
	for _, operation := range operations {
		candidate, candidateOK := paperByID[operation.CandidatePaperID]
		canonical, canonicalOK := paperByID[operation.CanonicalPaperID]
		if !candidateOK || !canonicalOK ||
			stringValue(candidate.Paper.DOI) == "" || stringValue(canonical.Paper.DOI) == "" ||
			metadata.TitlesMatch(candidate.Paper.Title, canonical.Paper.Title) {
			keptOperations = append(keptOperations, operation)
			continue
		}

		delete(deletedIDs, candidate.Paper.ID)
		conflictIDs[candidate.Paper.ID] = struct{}{}
		state := reviewStates[candidate.Paper.ID]
		if state == "" || (state != store.CleanupReviewStatePending && state != store.CleanupReviewStateKept && state != store.CleanupReviewStateDOIClear) {
			drafts = append(drafts, store.CleanupReviewDraft{
				GroupKey:         "doi-conflict:" + strconv.FormatInt(candidate.Paper.ID, 10),
				CandidatePaperID: candidate.Paper.ID,
				MatchedPaperID:   canonical.Paper.ID,
				MatchType:        store.CleanupReviewMatchDOIConflict,
				Reason:           "Both entries have DOI values, but their stored titles differ. This review only changes a DOI or keeps both entries unchanged; it does not delete either article.",
				SuggestedAction:  store.CleanupDecisionKeep,
			})
		}
	}
	return keptOperations, drafts, conflictIDs
}

func smallestPaperID(papers []store.CleanupPaper) int64 {
	minimum := int64(0)
	for _, item := range papers {
		if minimum == 0 || item.Paper.ID < minimum {
			minimum = item.Paper.ID
		}
	}
	return minimum
}

func exactKeyGroups(papers []store.CleanupPaper) [][]store.CleanupPaper {
	grouped := map[string][]store.CleanupPaper{}
	for _, item := range papers {
		if key := normalizedURL(item.Paper.URL); key != "" {
			grouped["url:"+key] = append(grouped["url:"+key], item)
		}
		if key := normalizedDOI(item.Paper.DOI); key != "" {
			grouped["doi:"+key] = append(grouped["doi:"+key], item)
		}
	}
	result := make([][]store.CleanupPaper, 0, len(grouped))
	for _, items := range grouped {
		result = append(result, items)
	}
	return result
}

func findTitleDuplicate(candidate store.CleanupPaper, papers []store.CleanupPaper, deletedIDs map[int64]struct{}) *store.CleanupPaper {
	matches := make([]store.CleanupPaper, 0)
	for _, item := range papers {
		if item.Paper.ID == candidate.Paper.ID {
			continue
		}
		if _, deleted := deletedIDs[item.Paper.ID]; deleted {
			continue
		}
		if !metadata.TitlesMatch(candidate.Paper.Title, item.Paper.Title) {
			continue
		}
		candidateDate := stringValue(candidate.Paper.PublishedDate)
		matchedDate := stringValue(item.Paper.PublishedDate)
		if candidateDate != "" && matchedDate != "" && !metadata.DatesMatch(candidateDate, matchedDate) {
			continue
		}
		matches = append(matches, item)
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Classified != matches[j].Classified {
			return matches[i].Classified
		}
		if !matches[i].Paper.FirstSeenAt.Equal(matches[j].Paper.FirstSeenAt) {
			return matches[i].Paper.FirstSeenAt.Before(matches[j].Paper.FirstSeenAt)
		}
		return matches[i].Paper.ID < matches[j].Paper.ID
	})
	return &matches[0]
}

func normalizedURL(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizedDOI(value *string) string {
	if value == nil {
		return ""
	}
	return metadata.NormalizeDOI(*value)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

const (
	cleanupBackupFilePrefix  = "literature.sqlite.pre-cleanup-"
	cleanupBackupRetainCount = 10
)

func cleanupBackupDir(settings config.Settings) string {
	directory := strings.TrimSpace(settings.DataDir)
	if directory == "" {
		return filepath.Dir(settings.DatabasePath)
	}
	return directory
}

func cleanupBackupPath(settings config.Settings, now time.Time) string {
	return filepath.Join(cleanupBackupDir(settings), cleanupBackupFilePrefix+strconv.FormatInt(now.UnixNano(), 10))
}

// pruneCleanupBackups bounds the disk cost of the per-run and per-decision
// VACUUM INTO snapshots by keeping only the newest ones. Pruning is best-effort
// housekeeping: failures never fail an otherwise successful cleanup mutation.
func pruneCleanupBackups(directory string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	type backupFile struct {
		name    string
		modTime time.Time
	}
	backups := []backupFile{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), cleanupBackupFilePrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backupFile{name: entry.Name(), modTime: info.ModTime()})
	}
	sort.SliceStable(backups, func(i, j int) bool {
		return backups[i].modTime.After(backups[j].modTime)
	})
	if len(backups) <= cleanupBackupRetainCount {
		return
	}
	for _, backup := range backups[cleanupBackupRetainCount:] {
		_ = os.Remove(filepath.Join(directory, backup.name))
	}
}
