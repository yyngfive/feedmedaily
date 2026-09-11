package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	CleanupReviewStatePending = "pending"
	// CleanupReviewStateDeferred is retained only for migrating databases created
	// before manual deferral was removed from the product flow.
	CleanupReviewStateDeferred = "deferred"
	CleanupReviewStateKept     = "kept"
	CleanupReviewStateDeleted  = "deleted"
	CleanupReviewStateDOIClear = "doi_cleared"

	CleanupReviewMatchTitle       = "title_duplicate"
	CleanupReviewMatchDOIConflict = "doi_conflict"
	CleanupReviewMatchDOIUnclear  = "doi_uncertain"

	CleanupDecisionKeep          = "keep"
	CleanupDecisionDelete        = "delete"
	CleanupDecisionDeleteMatch   = "delete_match"
	CleanupDecisionClearDOI      = "clear_doi"
	CleanupDecisionClearMatchDOI = "clear_match_doi"

	CleanupOperationDeleteDuplicate = "delete_duplicate"
	CleanupOperationClearDOI        = "clear_doi"
	CleanupIdentityURL              = "url"
	CleanupIdentityDOI              = "doi"
)

var (
	ErrCleanupReviewNotFound = errors.New("cleanup review not found")
	ErrCleanupReviewConflict = errors.New("cleanup review is no longer pending")
	ErrCleanupKeyCollision   = errors.New("repairing the paper DOI would collide with another paper key")
)

// CleanupPaper 是清理扫描使用的论文快照。把分类状态和 Paper 一起返回，
// 让扫描层无需为每个候选重新查询最新分类。
type CleanupPaper struct {
	Paper      Paper
	Classified bool
}

// CleanupPaperSummary 是人工复核界面需要的最小论文信息，不暴露 raw_json。
type CleanupPaperSummary struct {
	ID            int64     `json:"id"`
	Title         string    `json:"title"`
	URL           string    `json:"url"`
	DOI           *string   `json:"doi"`
	Journal       *string   `json:"journal"`
	PublishedDate *string   `json:"published_date"`
	FirstSeenAt   time.Time `json:"first_seen_at"`
	Classified    bool      `json:"classified"`
}

// CleanupReview 是一条持久化的人工复核项。candidate 始终是扫描到的未分类
// 文章；matched 是可选的已存在对照文章。
type CleanupReview struct {
	ID              int64                `json:"id"`
	Candidate       CleanupPaperSummary  `json:"candidate"`
	Matched         *CleanupPaperSummary `json:"matched,omitempty"`
	MatchType       string               `json:"match_type"`
	Reason          string               `json:"reason"`
	SuggestedAction string               `json:"suggested_action"`
	State           string               `json:"state"`
	Decision        *string              `json:"decision,omitempty"`
	CreatedAt       time.Time            `json:"created_at"`
	DecidedAt       *time.Time           `json:"decided_at,omitempty"`
}

// CleanupReviewDraft 是扫描层写入人工队列的窄接口。
type CleanupReviewDraft struct {
	GroupKey         string
	CandidatePaperID int64
	MatchedPaperID   int64
	MatchType        string
	Reason           string
	SuggestedAction  string
}

// CleanupOperation 是清理事务执行的确定性操作。CanonicalPaperID 仅用于
// delete_duplicate，clear_doi 操作将其置零。
type CleanupOperation struct {
	Kind             string
	CandidatePaperID int64
	CanonicalPaperID int64
	Identity         string
	Reason           string
}

type CleanupApplyResult struct {
	DeletedDuplicates int
	RepairedDOI       int
	Skipped           int
}

type CleanupDecisionResult struct {
	ReviewID       int64  `json:"review_id"`
	PaperID        int64  `json:"paper_id"`
	Decision       string `json:"decision"`
	Deleted        bool   `json:"deleted"`
	DeletedPaperID int64  `json:"deleted_paper_id,omitempty"`
	DOICleared     bool   `json:"doi_cleared"`
}

func (s *Store) UnclassifiedPaperCount() (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM papers`
	if len(s.classificationColumns) > 0 {
		query = `SELECT COUNT(*) FROM papers p WHERE NOT EXISTS (SELECT 1 FROM classifications c WHERE c.paper_id = p.id)`
	}
	if err := s.db.QueryRow(query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unclassified papers: %w", err)
	}
	return count, nil
}

// ListPapersForCleanup 批量读取论文和分类存在性，供全库清理扫描使用。
func (s *Store) ListPapersForCleanup() ([]CleanupPaper, error) {
	if len(s.paperColumns) == 0 {
		return []CleanupPaper{}, nil
	}
	rows, err := s.db.Query(s.cleanupPaperSelectSQL())
	if err != nil {
		return nil, fmt.Errorf("query papers for cleanup: %w", err)
	}
	defer rows.Close()

	papers := []CleanupPaper{}
	for rows.Next() {
		base, err := scanPaperRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan paper for cleanup: %w", err)
		}
		paper, err := paperFromPaperRow(base)
		if err != nil {
			return nil, fmt.Errorf("parse paper for cleanup: %w", err)
		}
		papers = append(papers, CleanupPaper{Paper: paper})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate papers for cleanup: %w", err)
	}

	classified := map[int64]struct{}{}
	if len(s.classificationColumns) > 0 {
		classificationRows, err := s.db.Query(`SELECT DISTINCT paper_id FROM classifications`)
		if err != nil && !isMissingSQLiteTable(err) {
			return nil, fmt.Errorf("query classified papers for cleanup: %w", err)
		}
		if err == nil {
			defer classificationRows.Close()
			for classificationRows.Next() {
				var paperID int64
				if err := classificationRows.Scan(&paperID); err != nil {
					return nil, fmt.Errorf("scan classified paper for cleanup: %w", err)
				}
				classified[paperID] = struct{}{}
			}
			if err := classificationRows.Err(); err != nil {
				return nil, fmt.Errorf("iterate classified papers for cleanup: %w", err)
			}
		}
	}
	for index := range papers {
		_, papers[index].Classified = classified[papers[index].Paper.ID]
	}
	return papers, nil
}

// CleanupReviewStates 返回每个候选论文最近一次复核状态，用于重复扫描时
// 保留 pending 队列，不把人工未处理项重新送入分类器。
func (s *Store) CleanupReviewStates() (map[int64]string, error) {
	result := map[int64]string{}
	rows, err := s.db.Query(`
		SELECT candidate_paper_id,
		       CASE WHEN state = ? THEN ? ELSE state END
		FROM cleanup_reviews
	`, CleanupReviewStateDeferred, CleanupReviewStatePending)
	if err != nil {
		if isMissingSQLiteTable(err) {
			return result, nil
		}
		return nil, fmt.Errorf("query cleanup review states: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var paperID int64
		var state string
		if err := rows.Scan(&paperID, &state); err != nil {
			return nil, fmt.Errorf("scan cleanup review state: %w", err)
		}
		if previous, exists := result[paperID]; !exists || cleanupReviewStatePriority(state) > cleanupReviewStatePriority(previous) {
			result[paperID] = state
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cleanup review states: %w", err)
	}
	return result, nil
}

func (s *Store) CountCleanupReviews(state string) (int, error) {
	var count int
	var err error
	if strings.TrimSpace(state) == "" {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM cleanup_reviews`).Scan(&count)
	} else {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM cleanup_reviews WHERE state = ?`, state).Scan(&count)
	}
	if err != nil {
		if isMissingSQLiteTable(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("count cleanup reviews: %w", err)
	}
	return count, nil
}

// UpsertCleanupReview 保留已解决记录的审计状态；同一候选重新变成未分类
// 时，扫描层可以读取该记录并再次尝试分类，而不会丢掉历史决定。
func (s *Store) UpsertCleanupReview(draft CleanupReviewDraft, now time.Time) (int64, error) {
	if strings.TrimSpace(draft.GroupKey) == "" {
		return 0, fmt.Errorf("cleanup review group key cannot be blank")
	}
	var matched any
	if draft.MatchedPaperID > 0 {
		matched = draft.MatchedPaperID
	}
	_, err := s.db.Exec(`
		INSERT INTO cleanup_reviews (
			group_key, candidate_paper_id, matched_paper_id, match_type,
			reason, suggested_action, state, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(group_key) DO UPDATE SET
			candidate_paper_id = excluded.candidate_paper_id,
			matched_paper_id = excluded.matched_paper_id,
			match_type = excluded.match_type,
			reason = excluded.reason,
			suggested_action = excluded.suggested_action
	`, draft.GroupKey, draft.CandidatePaperID, matched, draft.MatchType, draft.Reason, draft.SuggestedAction, CleanupReviewStatePending, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("upsert cleanup review: %w", err)
	}
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM cleanup_reviews WHERE group_key = ?`, draft.GroupKey).Scan(&id); err != nil {
		return 0, fmt.Errorf("read cleanup review id: %w", err)
	}
	return id, nil
}

func cleanupReviewStatePriority(state string) int {
	switch state {
	case CleanupReviewStatePending:
		return 3
	default:
		return 1
	}
}

func (s *Store) ListCleanupReviews(state string) ([]CleanupReview, error) {
	return s.queryCleanupReviews(strings.TrimSpace(state), nil)
}

func (s *Store) CleanupReviewByID(id int64) (*CleanupReview, error) {
	items, err := s.queryCleanupReviews("", &id)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrCleanupReviewNotFound
	}
	return &items[0], nil
}

func (s *Store) queryCleanupReviews(state string, id *int64) ([]CleanupReview, error) {
	query := fmt.Sprintf(`
		SELECT
			r.id, r.matched_paper_id, r.match_type, r.reason, r.suggested_action,
			r.state, r.decision, r.created_at, r.decided_at,
			p.id, p.title, p.url, p.doi, p.journal, p.published_date, p.first_seen_at,
			EXISTS (SELECT 1 FROM classifications cp WHERE cp.paper_id = p.id),
			m.id, m.title, m.url, m.doi, m.journal, m.published_date, m.first_seen_at,
			EXISTS (SELECT 1 FROM classifications cm WHERE cm.paper_id = m.id)
		FROM cleanup_reviews r
		JOIN papers p ON p.id = r.candidate_paper_id
		LEFT JOIN papers m ON m.id = r.matched_paper_id
		%s
		ORDER BY CASE r.state WHEN '%s' THEN 0 WHEN '%s' THEN 1 ELSE 2 END,
			r.created_at DESC, r.id DESC
	`, cleanupReviewWhere(state, id), CleanupReviewStatePending, CleanupReviewStateDeferred)
	args := cleanupReviewArgs(state, id)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		if isMissingSQLiteTable(err) {
			return []CleanupReview{}, nil
		}
		return nil, fmt.Errorf("query cleanup reviews: %w", err)
	}
	defer rows.Close()

	items := []CleanupReview{}
	for rows.Next() {
		var item CleanupReview
		var matchedID sql.NullInt64
		var decision sql.NullString
		var createdAt string
		var decidedAt sql.NullString
		var candidate CleanupPaperSummary
		var candidateDOI, candidateJournal, candidatePublished sql.NullString
		var candidateFirstSeen string
		var candidateClassified int64
		var matchedPaperID sql.NullInt64
		var matchedTitle, matchedURL, matchedDOI, matchedJournal, matchedPublished, matchedFirstSeen sql.NullString
		var matchedClassified int64
		if err := rows.Scan(
			&item.ID, &matchedID, &item.MatchType, &item.Reason, &item.SuggestedAction,
			&item.State, &decision, &createdAt, &decidedAt,
			&candidate.ID, &candidate.Title, &candidate.URL, &candidateDOI, &candidateJournal, &candidatePublished, &candidateFirstSeen,
			&candidateClassified,
			&matchedPaperID, &matchedTitle, &matchedURL, &matchedDOI, &matchedJournal, &matchedPublished, &matchedFirstSeen,
			&matchedClassified,
		); err != nil {
			return nil, fmt.Errorf("scan cleanup review: %w", err)
		}
		parsedCreatedAt, err := parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse cleanup review created_at: %w", err)
		}
		candidate.DOI = nullableString(candidateDOI)
		candidate.Journal = nullableString(candidateJournal)
		candidate.PublishedDate = nullableString(candidatePublished)
		candidate.Classified = candidateClassified != 0
		candidate.FirstSeenAt, err = parseTime(candidateFirstSeen)
		if err != nil {
			return nil, fmt.Errorf("parse cleanup candidate first_seen_at: %w", err)
		}
		item.Candidate = candidate
		item.Decision = nullableString(decision)
		item.CreatedAt = parsedCreatedAt
		if decidedAt.Valid && strings.TrimSpace(decidedAt.String) != "" {
			parsed, err := parseTime(decidedAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse cleanup review decided_at: %w", err)
			}
			item.DecidedAt = &parsed
		}
		if matchedID.Valid && matchedPaperID.Valid {
			matchedFirstSeenAt, err := parseTime(matchedFirstSeen.String)
			if err != nil {
				return nil, fmt.Errorf("parse cleanup matched first_seen_at: %w", err)
			}
			item.Matched = &CleanupPaperSummary{
				ID:            matchedPaperID.Int64,
				Title:         matchedTitle.String,
				URL:           matchedURL.String,
				DOI:           nullableString(matchedDOI),
				Journal:       nullableString(matchedJournal),
				PublishedDate: nullableString(matchedPublished),
				FirstSeenAt:   matchedFirstSeenAt,
				Classified:    matchedClassified != 0,
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cleanup reviews: %w", err)
	}
	return items, nil
}

func cleanupReviewWhere(state string, id *int64) string {
	conditions := []string{}
	if state != "" {
		conditions = append(conditions, "r.state = ?")
	}
	if id != nil {
		conditions = append(conditions, "r.id = ?")
	}
	if len(conditions) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conditions, " AND ")
}

func cleanupReviewArgs(state string, id *int64) []any {
	args := []any{}
	if state != "" {
		args = append(args, state)
	}
	if id != nil {
		args = append(args, *id)
	}
	return args
}

// ApplyCleanupOperations 在一个 SQLite 事务内执行确定性清理。每条删除
// 都再次确认候选仍未分类，避免扫描期间的外部写入被误删。
func (s *Store) ApplyCleanupOperations(ctx context.Context, operations []CleanupOperation, now time.Time) (CleanupApplyResult, error) {
	result := CleanupApplyResult{}
	if len(operations) == 0 {
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin cleanup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, operation := range operations {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		candidate, err := s.paperByIDTx(tx, operation.CandidatePaperID)
		if err != nil {
			return result, err
		}
		if candidate == nil || s.paperHasClassificationTx(tx, operation.CandidatePaperID) {
			result.Skipped++
			continue
		}
		switch operation.Kind {
		case CleanupOperationDeleteDuplicate:
			if operation.CanonicalPaperID <= 0 || operation.CanonicalPaperID == operation.CandidatePaperID {
				return result, fmt.Errorf("invalid cleanup canonical paper for %d", operation.CandidatePaperID)
			}
			canonical, err := s.paperByIDTx(tx, operation.CanonicalPaperID)
			if err != nil {
				return result, err
			}
			if canonical == nil {
				result.Skipped++
				continue
			}
			if err := s.mergeDuplicatePaperTx(tx, *canonical, *candidate, operation.Identity, now); err != nil {
				return result, err
			}
			if err := s.repointPaperReferencesTx(tx, operation.CandidatePaperID, operation.CanonicalPaperID); err != nil {
				return result, err
			}
			if err := s.markCleanupReviewsDeletedTx(tx, operation.CandidatePaperID, now); err != nil {
				return result, err
			}
			if _, err := tx.Exec(`DELETE FROM papers WHERE id = ?`, operation.CandidatePaperID); err != nil {
				return result, fmt.Errorf("delete duplicate paper %d: %w", operation.CandidatePaperID, err)
			}
			result.DeletedDuplicates++
		case CleanupOperationClearDOI:
			if err := s.clearPaperDOITx(tx, operation.CandidatePaperID); err != nil {
				return result, err
			}
			result.RepairedDOI++
		default:
			return result, fmt.Errorf("unsupported cleanup operation: %s", operation.Kind)
		}
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit cleanup transaction: %w", err)
	}
	return result, nil
}

// ApplyCleanupReviewDecision applies one human decision transactionally and
// leaves the review row as an audit record. Keep/clear-DOI decisions leave the
// paper without a classification for the next cleanup batch.
func (s *Store) ApplyCleanupReviewDecision(ctx context.Context, reviewID int64, decision string, now time.Time) (CleanupDecisionResult, error) {
	result := CleanupDecisionResult{ReviewID: reviewID, Decision: decision}
	if ctx == nil {
		ctx = context.Background()
	}
	if decision != CleanupDecisionKeep && decision != CleanupDecisionDelete && decision != CleanupDecisionDeleteMatch && decision != CleanupDecisionClearDOI && decision != CleanupDecisionClearMatchDOI {
		return result, fmt.Errorf("unsupported cleanup review decision: %s", decision)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin cleanup review transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var candidateID int64
	var matchedID sql.NullInt64
	var matchType string
	var state string
	err = tx.QueryRow(`
		SELECT candidate_paper_id, matched_paper_id, match_type, state
		FROM cleanup_reviews WHERE id = ?
	`, reviewID).Scan(&candidateID, &matchedID, &matchType, &state)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isMissingSQLiteTable(err) {
			return result, ErrCleanupReviewNotFound
		}
		return result, fmt.Errorf("read cleanup review %d: %w", reviewID, err)
	}
	if state != CleanupReviewStatePending && state != CleanupReviewStateDeferred {
		return result, ErrCleanupReviewConflict
	}
	switch matchType {
	case CleanupReviewMatchTitle:
		if decision != CleanupDecisionKeep && decision != CleanupDecisionDelete && decision != CleanupDecisionDeleteMatch {
			return result, fmt.Errorf("title duplicate reviews only support keep, delete, or delete_match")
		}
	case CleanupReviewMatchDOIConflict:
		if decision != CleanupDecisionKeep && decision != CleanupDecisionClearDOI && decision != CleanupDecisionClearMatchDOI {
			return result, fmt.Errorf("DOI conflict reviews only support keep, clear_doi, or clear_match_doi")
		}
	case CleanupReviewMatchDOIUnclear:
		if decision != CleanupDecisionKeep && decision != CleanupDecisionClearDOI {
			return result, fmt.Errorf("uncertain DOI reviews only support keep or clear_doi")
		}
	}
	candidate, err := s.paperByIDTx(tx, candidateID)
	if err != nil {
		return result, err
	}
	if candidate == nil {
		return result, ErrCleanupReviewNotFound
	}
	if s.paperHasClassificationTx(tx, candidateID) {
		return result, fmt.Errorf("paper %d is already classified", candidateID)
	}
	result.PaperID = candidateID

	switch decision {
	case CleanupDecisionDelete, CleanupDecisionDeleteMatch:
		if !matchedID.Valid || matchedID.Int64 <= 0 || matchedID.Int64 == candidateID {
			return result, fmt.Errorf("cleanup review %d has no matched paper to retain", reviewID)
		}
		matched, err := s.paperByIDTx(tx, matchedID.Int64)
		if err != nil {
			return result, err
		}
		if matched == nil {
			return result, ErrCleanupReviewNotFound
		}
		canonicalID := matchedID.Int64
		duplicateID := candidateID
		canonical := *matched
		duplicate := *candidate
		if decision == CleanupDecisionDeleteMatch {
			canonicalID = candidateID
			duplicateID = matchedID.Int64
			canonical = *candidate
			duplicate = *matched
		}
		if err := s.mergeDuplicatePaperTx(tx, canonical, duplicate, "title", now); err != nil {
			return result, err
		}
		if err := s.repointPaperReferencesTx(tx, duplicateID, canonicalID); err != nil {
			return result, err
		}
		if decision == CleanupDecisionDeleteMatch {
			if err := s.deletePaperClassificationsTx(tx, duplicateID); err != nil {
				return result, err
			}
		}
		if err := s.markCleanupReviewsDeletedTx(tx, duplicateID, now); err != nil {
			return result, err
		}
		if _, err := tx.Exec(`DELETE FROM papers WHERE id = ?`, duplicateID); err != nil {
			return result, fmt.Errorf("delete reviewed duplicate paper %d: %w", duplicateID, err)
		}
		result.Deleted = true
		result.DeletedPaperID = duplicateID
	case CleanupDecisionClearDOI:
		if err := s.clearPaperDOITx(tx, candidateID); err != nil {
			return result, err
		}
		result.DOICleared = true
	case CleanupDecisionClearMatchDOI:
		if !matchedID.Valid || matchedID.Int64 <= 0 || matchedID.Int64 == candidateID {
			return result, fmt.Errorf("cleanup review %d has no matched paper whose DOI can be cleared", reviewID)
		}
		matched, err := s.paperByIDTx(tx, matchedID.Int64)
		if err != nil {
			return result, err
		}
		if matched == nil {
			return result, ErrCleanupReviewNotFound
		}
		if err := s.clearPaperDOITx(tx, matchedID.Int64); err != nil {
			return result, err
		}
		result.DOICleared = true
	}

	nextState := CleanupReviewStateKept
	switch decision {
	case CleanupDecisionDelete:
		nextState = CleanupReviewStateDeleted
	case CleanupDecisionDeleteMatch:
		nextState = CleanupReviewStateKept
	case CleanupDecisionClearDOI:
		nextState = CleanupReviewStateDOIClear
	case CleanupDecisionClearMatchDOI:
		nextState = CleanupReviewStateDOIClear
	}
	if _, err := tx.Exec(`
		UPDATE cleanup_reviews
		SET state = ?, decision = ?, decided_at = ?
		WHERE id = ? AND state IN (?, ?)
	`, nextState, decision, now.UTC().Format(time.RFC3339Nano), reviewID, CleanupReviewStatePending, CleanupReviewStateDeferred); err != nil {
		return result, fmt.Errorf("record cleanup review decision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit cleanup review transaction: %w", err)
	}
	return result, nil
}

// BackupTo creates a consistent SQLite snapshot before a cleanup mutation.
// VACUUM INTO includes WAL state and refuses to overwrite an existing file.
func (s *Store) BackupTo(path string) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "." || clean == "" {
		return fmt.Errorf("backup path cannot be blank")
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if _, err := os.Stat(clean); err == nil {
		return fmt.Errorf("backup file already exists: %s", clean)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check backup path: %w", err)
	}
	if _, err := s.db.Exec(`VACUUM INTO ?`, clean); err != nil {
		return fmt.Errorf("backup sqlite database: %w", err)
	}
	return nil
}

func (s *Store) cleanupPaperSelectSQL() string {
	return fmt.Sprintf(`
		SELECT id, source_url, feed_title, title, url, doi, journal, authors_json, abstract,
			%s AS abstract_source, published_date, first_seen_at, %s AS read_at, raw_json
		FROM papers
		ORDER BY first_seen_at DESC, id DESC
	`, s.columnExpr(s.paperColumns, "abstract_source", quote(abstractSourceNone)), s.columnExpr(s.paperColumns, "read_at", "NULL"))
}

func (s *Store) paperByIDTx(tx *sql.Tx, paperID int64) (*Paper, error) {
	row := tx.QueryRow(fmt.Sprintf(`
		SELECT id, source_url, feed_title, title, url, doi, journal, authors_json, abstract,
			%s AS abstract_source, published_date, first_seen_at, %s AS read_at, raw_json
		FROM papers WHERE id = ?
	`, s.columnExpr(s.paperColumns, "abstract_source", quote(abstractSourceNone)), s.columnExpr(s.paperColumns, "read_at", "NULL")), paperID)
	base, err := scanPaperRow(row)
	if err != nil {
		if strings.Contains(err.Error(), "sql: no rows in result set") || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query paper %d in cleanup transaction: %w", paperID, err)
	}
	paper, err := paperFromPaperRow(base)
	if err != nil {
		return nil, err
	}
	return &paper, nil
}

func (s *Store) paperHasClassificationTx(tx *sql.Tx, paperID int64) bool {
	if len(s.classificationColumns) == 0 {
		return false
	}
	var exists int
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM classifications WHERE paper_id = ?)`, paperID).Scan(&exists); err != nil {
		return true
	}
	return exists != 0
}

func (s *Store) deletePaperClassificationsTx(tx *sql.Tx, paperID int64) error {
	if len(s.classificationColumns) == 0 {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM classifications WHERE paper_id = ?`, paperID); err != nil {
		return fmt.Errorf("delete classifications for paper %d: %w", paperID, err)
	}
	return nil
}

func mergeDuplicatePaperContent(existing Paper, duplicate Paper, identity string) Paper {
	result := existing
	if result.FeedTitle == nil && duplicate.FeedTitle != nil {
		value := *duplicate.FeedTitle
		result.FeedTitle = &value
	}
	if result.Journal == nil && duplicate.Journal != nil {
		value := *duplicate.Journal
		result.Journal = &value
	}
	if len(result.Authors) == 0 && len(duplicate.Authors) > 0 {
		result.Authors = append([]string{}, duplicate.Authors...)
	}
	if !paperHasAbstractContent(result) && paperHasAbstractContent(duplicate) {
		result.Abstract = duplicate.Abstract
		result.AbstractHTML = duplicate.AbstractHTML
		result.AbstractImages = append([]AbstractImage{}, duplicate.AbstractImages...)
		result.AbstractSource = duplicate.AbstractSource
	}
	if result.PublishedDate == nil && duplicate.PublishedDate != nil {
		value := *duplicate.PublishedDate
		result.PublishedDate = &value
	}
	if result.ReadAt == nil && duplicate.ReadAt != nil {
		value := *duplicate.ReadAt
		result.ReadAt = &value
	}
	if identity == CleanupIdentityDOI && result.DOI == nil && duplicate.DOI != nil {
		value := *duplicate.DOI
		result.DOI = &value
	}
	if result.Raw == nil {
		result.Raw = map[string]any{}
	}
	for key, value := range duplicate.Raw {
		if _, exists := result.Raw[key]; !exists {
			result.Raw[key] = value
		}
	}
	return result
}

func (s *Store) mergeDuplicatePaperTx(tx *sql.Tx, canonical Paper, duplicate Paper, identity string, now time.Time) error {
	merged := mergeDuplicatePaperContent(canonical, duplicate, identity)
	rawJSON, authorsJSON, err := encodeStoredPaper(merged)
	if err != nil {
		return err
	}
	assignments := []string{
		"source_url = ?", "feed_title = ?", "title = ?", "url = ?", "doi = ?", "journal = ?",
		"authors_json = ?", "abstract = ?", "raw_json = ?",
	}
	args := []any{merged.SourceURL, merged.FeedTitle, merged.Title, merged.URL, merged.DOI, merged.Journal, authorsJSON, merged.Abstract, rawJSON}
	if s.paperColumns["abstract_source"] {
		assignments = append(assignments, "abstract_source = ?")
		args = append(args, normalizeAbstractSource(merged.AbstractSource))
	}
	if s.paperColumns["published_date"] {
		assignments = append(assignments, "published_date = ?")
		args = append(args, merged.PublishedDate)
	}
	if s.paperColumns["read_at"] {
		assignments = append(assignments, "read_at = ?")
		args = append(args, formatNullableTime(merged.ReadAt))
	}
	if s.paperColumns["last_checked_at"] {
		assignments = append(assignments, "last_checked_at = ?")
		args = append(args, now.UTC().Format(time.RFC3339Nano))
	}
	args = append(args, canonical.ID)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE papers SET %s WHERE id = ?`, strings.Join(assignments, ", ")), args...); err != nil {
		return fmt.Errorf("merge duplicate paper %d into %d: %w", duplicate.ID, canonical.ID, err)
	}
	return nil
}

func (s *Store) clearPaperDOITx(tx *sql.Tx, paperID int64) error {
	var urlValue, title string
	if err := tx.QueryRow(`SELECT url, title FROM papers WHERE id = ?`, paperID).Scan(&urlValue, &title); err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "sql: no rows in result set") {
			return ErrPaperNotFound
		}
		return fmt.Errorf("read paper %d before clearing DOI: %w", paperID, err)
	}
	if !s.paperColumns["paper_key"] {
		if _, err := tx.Exec(`UPDATE papers SET doi = NULL WHERE id = ?`, paperID); err != nil {
			return fmt.Errorf("clear paper doi %d: %w", paperID, err)
		}
		return nil
	}
	key := paperKey(Paper{Title: title, URL: urlValue})
	var collisionID int64
	err := tx.QueryRow(`SELECT id FROM papers WHERE paper_key = ? AND id <> ? LIMIT 1`, key, paperID).Scan(&collisionID)
	if err == nil {
		return fmt.Errorf("%w: paper %d conflicts with paper %d", ErrCleanupKeyCollision, paperID, collisionID)
	}
	if !errors.Is(err, sql.ErrNoRows) && !strings.Contains(err.Error(), "sql: no rows in result set") {
		return fmt.Errorf("check paper key collision for %d: %w", paperID, err)
	}
	if _, err := tx.Exec(`UPDATE papers SET doi = NULL, paper_key = ? WHERE id = ?`, key, paperID); err != nil {
		return fmt.Errorf("clear paper doi and repair key %d: %w", paperID, err)
	}
	return nil
}

func (s *Store) repointPaperReferencesTx(tx *sql.Tx, fromID int64, toID int64) error {
	if len(s.feedbackColumns) > 0 {
		if _, err := tx.Exec(`UPDATE feedback SET paper_id = ? WHERE paper_id = ?`, toID, fromID); err != nil {
			return fmt.Errorf("repoint feedback from paper %d: %w", fromID, err)
		}
	}
	if err := s.mergeZoteroReferencesTx(tx, fromID, toID); err != nil {
		return err
	}
	return nil
}

func (s *Store) markCleanupReviewsDeletedTx(tx *sql.Tx, paperID int64, now time.Time) error {
	_, err := tx.Exec(`
		UPDATE cleanup_reviews
		SET state = ?, decision = ?, decided_at = ?
		WHERE candidate_paper_id = ? AND state IN (?, ?)
	`, CleanupReviewStateDeleted, CleanupDecisionDelete, now.UTC().Format(time.RFC3339Nano), paperID, CleanupReviewStatePending, CleanupReviewStateDeferred)
	if err != nil && !isMissingSQLiteTable(err) {
		return fmt.Errorf("close cleanup reviews for deleted paper %d: %w", paperID, err)
	}
	return nil
}

type cleanupZoteroRow struct {
	State       sql.NullString
	ItemKey     sql.NullString
	Error       sql.NullString
	AttemptedAt sql.NullString
	SavedAt     sql.NullString
}

func (s *Store) queryZoteroRowTx(tx *sql.Tx, paperID int64) (*cleanupZoteroRow, error) {
	if len(s.zoteroColumns) == 0 {
		return nil, nil
	}
	row := tx.QueryRow(fmt.Sprintf(`
		SELECT %s, %s, %s, %s, %s
		FROM zotero_saves WHERE paper_id = ?
	`, s.columnExpr(s.zoteroColumns, "state", "NULL"), s.columnExpr(s.zoteroColumns, "item_key", "NULL"), s.columnExpr(s.zoteroColumns, "error_message", "NULL"), s.columnExpr(s.zoteroColumns, "attempted_at", "NULL"), s.columnExpr(s.zoteroColumns, "saved_at", "NULL")), paperID)
	var item cleanupZoteroRow
	if err := row.Scan(&item.State, &item.ItemKey, &item.Error, &item.AttemptedAt, &item.SavedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "sql: no rows in result set") {
			return nil, nil
		}
		return nil, fmt.Errorf("query zotero status for paper %d during cleanup: %w", paperID, err)
	}
	return &item, nil
}

func (s *Store) mergeZoteroReferencesTx(tx *sql.Tx, fromID int64, toID int64) error {
	from, err := s.queryZoteroRowTx(tx, fromID)
	if err != nil || from == nil {
		return err
	}
	to, err := s.queryZoteroRowTx(tx, toID)
	if err != nil {
		return err
	}
	if to == nil {
		if _, err := tx.Exec(`UPDATE zotero_saves SET paper_id = ? WHERE paper_id = ?`, toID, fromID); err != nil {
			return fmt.Errorf("repoint Zotero status from paper %d: %w", fromID, err)
		}
		return nil
	}
	if cleanupZoteroRowShouldReplace(*to, *from) {
		assignments := []string{"paper_id = ?"}
		args := []any{toID}
		for _, field := range []struct {
			name  string
			value any
		}{
			{"state", nullableAny(from.State)}, {"item_key", nullableAny(from.ItemKey)}, {"error_message", nullableAny(from.Error)},
			{"attempted_at", nullableAny(from.AttemptedAt)}, {"saved_at", nullableAny(from.SavedAt)},
		} {
			if s.zoteroColumns[field.name] {
				assignments = append(assignments, field.name+" = ?")
				args = append(args, field.value)
			}
		}
		args = append(args, toID)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE zotero_saves SET %s WHERE paper_id = ?`, strings.Join(assignments, ", ")), args...); err != nil {
			return fmt.Errorf("merge Zotero status into paper %d: %w", toID, err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM zotero_saves WHERE paper_id = ?`, fromID); err != nil {
		return fmt.Errorf("delete duplicate Zotero status for paper %d: %w", fromID, err)
	}
	return nil
}

func cleanupZoteroRowShouldReplace(existing cleanupZoteroRow, incoming cleanupZoteroRow) bool {
	if incoming.State.Valid && incoming.State.String == zoteroStateSaved && (!existing.State.Valid || existing.State.String != zoteroStateSaved) {
		return true
	}
	if !incoming.AttemptedAt.Valid {
		return false
	}
	if !existing.AttemptedAt.Valid {
		return true
	}
	return incoming.AttemptedAt.String > existing.AttemptedAt.String
}

func nullableAny(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func isMissingSQLiteTable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}
