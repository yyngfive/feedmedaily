package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const writableSchema = `
CREATE TABLE IF NOT EXISTS papers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_key TEXT NOT NULL UNIQUE,
  doi TEXT,
  title TEXT NOT NULL,
  journal TEXT,
  url TEXT NOT NULL,
  source_url TEXT NOT NULL,
  feed_title TEXT,
  authors_json TEXT NOT NULL,
  abstract TEXT,
  abstract_source TEXT NOT NULL DEFAULT 'none',
  published_date TEXT,
  first_seen_at TEXT NOT NULL,
  read_at TEXT,
  last_checked_at TEXT NOT NULL,
  raw_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS classifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL,
  relevance TEXT NOT NULL,
  confidence REAL NOT NULL,
  reason TEXT NOT NULL,
  topic_tags_json TEXT NOT NULL,
  recommended_action TEXT NOT NULL,
  model TEXT NOT NULL,
  translated_title_zh TEXT,
  classified_at TEXT NOT NULL,
  FOREIGN KEY (paper_id) REFERENCES papers(id)
);

CREATE TABLE IF NOT EXISTS feedback (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL,
  original_relevance TEXT NOT NULL,
  corrected_relevance TEXT NOT NULL,
  original_topic TEXT,
  corrected_topic TEXT,
  note TEXT,
  state TEXT NOT NULL DEFAULT 'open',
  used_in_prompt INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  FOREIGN KEY (paper_id) REFERENCES papers(id)
);

CREATE TABLE IF NOT EXISTS profile_proposals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  summary TEXT NOT NULL,
  proposed_profile_json TEXT NOT NULL,
  rule_delta_json TEXT,
  base_profile_version INTEGER,
  change_set_json TEXT,
  source_feedback_ids_json TEXT NOT NULL,
  model TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL,
  applied_at TEXT,
  rejected_at TEXT,
  applied_version INTEGER,
  applied_profile_json TEXT
);

CREATE TABLE IF NOT EXISTS zotero_saves (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL UNIQUE,
  state TEXT NOT NULL,
  item_key TEXT,
  error_message TEXT,
  attempted_at TEXT NOT NULL,
  saved_at TEXT,
  FOREIGN KEY (paper_id) REFERENCES papers(id)
);

CREATE TABLE IF NOT EXISTS llm_usage_jobs (
  job_id TEXT PRIMARY KEY,
  job_type TEXT NOT NULL,
  status TEXT NOT NULL,
  model TEXT NOT NULL,
  request_count INTEGER NOT NULL,
  prompt_tokens INTEGER NOT NULL,
  prompt_cache_hit_tokens INTEGER NOT NULL,
  prompt_cache_miss_tokens INTEGER NOT NULL,
  completion_tokens INTEGER NOT NULL,
  pricing_status TEXT NOT NULL,
  pricing_json TEXT NOT NULL,
  estimated_cost_nano_cny INTEGER,
  estimated_cost_cny TEXT,
  completed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cleanup_reviews (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  group_key TEXT NOT NULL UNIQUE,
  candidate_paper_id INTEGER NOT NULL,
  matched_paper_id INTEGER,
  match_type TEXT NOT NULL,
  reason TEXT NOT NULL,
  suggested_action TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  decision TEXT,
  created_at TEXT NOT NULL,
  decided_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_papers_first_seen_at ON papers(first_seen_at);
CREATE INDEX IF NOT EXISTS idx_classifications_paper_id ON classifications(paper_id);
CREATE INDEX IF NOT EXISTS idx_classifications_paper_id_classified_at ON classifications(paper_id, classified_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_feedback_paper_id ON feedback(paper_id);
CREATE INDEX IF NOT EXISTS idx_feedback_created_at ON feedback(created_at);
CREATE INDEX IF NOT EXISTS idx_feedback_paper_id_state_created_at ON feedback(paper_id, state, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_profile_proposals_created_at ON profile_proposals(created_at);
CREATE INDEX IF NOT EXISTS idx_llm_usage_jobs_completed_at ON llm_usage_jobs(completed_at DESC);
CREATE INDEX IF NOT EXISTS idx_cleanup_reviews_state_created_at ON cleanup_reviews(state, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_cleanup_reviews_candidate_paper_id ON cleanup_reviews(candidate_paper_id);
`

func OpenOrCreate(path string) (*Store, error) {
	// 打开一个可写 SQLite 库；文件缺失时自动创建并初始化当前兼容 schema。
	clean := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite parent dir: %w", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(clean))
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	configureSQLiteDB(db, storePoolRoleWrite)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}
	if _, err := db.Exec(writableSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize sqlite schema: %w", err)
	}
	if err := ensureMutableSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repairSupersededPricing(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repairIncompleteCacheBreakdownPricing(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return buildStore(db)
}

func ensureMutableSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		UPDATE cleanup_reviews
		SET state = ?
		WHERE state = ?
	`, CleanupReviewStatePending, CleanupReviewStateDeferred); err != nil && !isMissingSQLiteTable(err) {
		return fmt.Errorf("migrate deferred cleanup reviews: %w", err)
	}
	if _, err := db.Exec(`
		UPDATE cleanup_reviews
		SET match_type = ?,
			suggested_action = ?,
			reason = ?
		WHERE state = ?
		  AND match_type = ?
		  AND group_key LIKE 'title:%'
	`, CleanupReviewMatchTitle, CleanupDecisionDelete,
		"The normalized title matches another paper, but no exact URL or DOI match was found. Confirm whether this is a duplicate. The DOI is left unchanged until this title match is reviewed.",
		CleanupReviewStatePending, CleanupReviewMatchDOIConflict); err != nil && !isMissingSQLiteTable(err) {
		return fmt.Errorf("migrate legacy DOI-conflict title reviews: %w", err)
	}
	if err := closeCleanupReviewsWithDeletedMatchedPapers(db); err != nil {
		return err
	}
	if err := migrateLegacyTitleReviewGroupKeys(db); err != nil {
		return err
	}
	if err := ensureColumn(db, "profile_proposals", "base_profile_version", "INTEGER"); err != nil {
		return err
	}
	if err := ensureColumn(db, "profile_proposals", "change_set_json", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(db, "profile_proposals", "applied_profile_json", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(db, "feedback", "original_topic", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(db, "feedback", "corrected_topic", "TEXT"); err != nil {
		return err
	}
	return nil
}

// closeCleanupReviewsWithDeletedMatchedPapers repairs pending reviews that
// reference a paper which no longer exists. Earlier versions only closed
// reviews whose candidate was deleted, so such rows could render without any
// actionable decision and pause bulk classification forever. Closing them lets
// the next cleanup scan rebuild an actionable review against a surviving paper.
func closeCleanupReviewsWithDeletedMatchedPapers(db *sql.DB) error {
	if _, err := db.Exec(`
		UPDATE cleanup_reviews
		SET state = ?, decision = ?, decided_at = ?
		WHERE state IN (?, ?)
			AND matched_paper_id IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM papers WHERE papers.id = cleanup_reviews.matched_paper_id)
	`, CleanupReviewStateDeleted, CleanupDecisionDelete, time.Now().UTC().Format(time.RFC3339Nano),
		CleanupReviewStatePending, CleanupReviewStateDeferred); err != nil && !isMissingSQLiteTable(err) {
		return fmt.Errorf("close cleanup reviews with deleted matched papers: %w", err)
	}
	return nil
}

// migrateLegacyTitleReviewGroupKeys rewrites pre-pair title review keys
// (title:<candidate>) to direction-independent pair keys. Mirrored rows for the
// same pair collapse into one row, preferring the pending one, so a decision
// recorded against either direction is honored.
func migrateLegacyTitleReviewGroupKeys(db *sql.DB) error {
	rows, err := db.Query(`
		SELECT id, candidate_paper_id, matched_paper_id, state
		FROM cleanup_reviews
		WHERE group_key = 'title:' || candidate_paper_id
			AND matched_paper_id IS NOT NULL
	`)
	if err != nil {
		if isMissingSQLiteTable(err) {
			return nil
		}
		return fmt.Errorf("query legacy title review keys: %w", err)
	}
	type legacyTitleReview struct {
		id        int64
		candidate int64
		matched   int64
		state     string
	}
	legacy := []legacyTitleReview{}
	for rows.Next() {
		var review legacyTitleReview
		if err := rows.Scan(&review.id, &review.candidate, &review.matched, &review.state); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan legacy title review: %w", err)
		}
		legacy = append(legacy, review)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate legacy title reviews: %w", err)
	}
	rows.Close()

	byPairKey := map[string][]legacyTitleReview{}
	for _, review := range legacy {
		key := CleanupTitleReviewGroupKey(review.candidate, review.matched)
		byPairKey[key] = append(byPairKey[key], review)
	}
	for key, group := range byPairKey {
		keep := group[0]
		for _, review := range group[1:] {
			if cleanupReviewStatePriority(review.state) > cleanupReviewStatePriority(keep.state) ||
				(cleanupReviewStatePriority(review.state) == cleanupReviewStatePriority(keep.state) && review.id < keep.id) {
				keep = review
			}
		}
		for _, review := range group {
			if review.id == keep.id {
				continue
			}
			if _, err := db.Exec(`DELETE FROM cleanup_reviews WHERE id = ?`, review.id); err != nil {
				return fmt.Errorf("delete superseded legacy title review %d: %w", review.id, err)
			}
		}
		if _, err := db.Exec(`UPDATE cleanup_reviews SET group_key = ? WHERE id = ?`, key, keep.id); err != nil {
			return fmt.Errorf("migrate legacy title review key %d: %w", keep.id, err)
		}
	}
	return nil
}

func ensureColumn(db *sql.DB, table string, column string, definition string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return fmt.Errorf("query sqlite table info for %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan sqlite table info for %s: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sqlite table info for %s: %w", table, err)
	}
	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add sqlite column %s.%s: %w", table, column, err)
	}
	return nil
}
