package sqlite

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenReadAndWriteConfigureSQLiteConcurrency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")

	writableStore, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if stats := writableStore.db.Stats(); stats.MaxOpenConnections != 1 {
		t.Fatalf("expected writable store max open connections to be 1, got %d", stats.MaxOpenConnections)
	}
	assertSQLitePragmas(t, writableStore.db)
	if err := writableStore.Close(); err != nil {
		t.Fatal(err)
	}

	readOnlyStore, err := OpenRead(path)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnlyStore.Close()
	if stats := readOnlyStore.db.Stats(); stats.MaxOpenConnections != 4 {
		t.Fatalf("expected read store max open connections to be 4, got %d", stats.MaxOpenConnections)
	}
	assertSQLitePragmas(t, readOnlyStore.db)

	writeOnlyStore, err := OpenWrite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writeOnlyStore.Close()
	if stats := writeOnlyStore.db.Stats(); stats.MaxOpenConnections != 1 {
		t.Fatalf("expected write store max open connections to be 1, got %d", stats.MaxOpenConnections)
	}
	assertSQLitePragmas(t, writeOnlyStore.db)
}

func assertSQLitePragmas(t *testing.T, db *sql.DB) {
	t.Helper()
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("expected journal_mode WAL, got %q", journalMode)
	}
	var busyTimeout int
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busyTimeout != sqliteBusyTimeoutMS {
		t.Fatalf("expected busy_timeout %d, got %d", sqliteBusyTimeoutMS, busyTimeout)
	}
}

func openSQLiteTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func execSQLite(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func seedMutableFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	execSQLite(t, db, `
CREATE TABLE papers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_url TEXT NOT NULL,
  feed_title TEXT,
  title TEXT NOT NULL,
  url TEXT NOT NULL,
  doi TEXT,
  journal TEXT,
  authors_json TEXT NOT NULL,
  abstract TEXT,
  abstract_source TEXT NOT NULL DEFAULT 'none',
  published_date TEXT,
  first_seen_at TEXT NOT NULL,
  read_at TEXT,
  raw_json TEXT NOT NULL
);
CREATE TABLE classifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL,
  relevance TEXT NOT NULL,
  confidence REAL NOT NULL,
  reason TEXT NOT NULL,
  topic_tags_json TEXT NOT NULL,
  recommended_action TEXT NOT NULL,
  model TEXT NOT NULL,
  translated_title_zh TEXT,
  classified_at TEXT NOT NULL
);
CREATE TABLE feedback (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL,
  original_relevance TEXT NOT NULL,
  corrected_relevance TEXT NOT NULL,
  note TEXT,
  state TEXT NOT NULL DEFAULT 'open',
  used_in_prompt INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE TABLE profile_proposals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  summary TEXT NOT NULL,
  proposed_profile_json TEXT NOT NULL,
  rule_delta_json TEXT,
  source_feedback_ids_json TEXT NOT NULL,
  model TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL,
  applied_at TEXT,
  rejected_at TEXT,
  applied_version INTEGER
);
`)
	execSQLite(t, db, `
INSERT INTO papers (
  id, source_url, feed_title, title, url, doi, journal, authors_json, abstract, abstract_source, published_date, first_seen_at, read_at, raw_json
) VALUES (
  1, 'https://example.com/rss', 'Feed', 'Mutable paper', 'https://example.com/mutable', NULL, NULL, '[]', NULL, 'none', NULL, '2026-05-16T01:00:00Z', NULL, '{}'
);
INSERT INTO classifications (
  paper_id, relevance, confidence, reason, topic_tags_json, recommended_action, model, translated_title_zh, classified_at
) VALUES (
  1, 'indirect', 0.8, 'Fixture', '[]', 'scan', 'test-model', NULL, '2026-05-16T02:00:00Z'
);
`)
}

func stringPointer(value string) *string {
	return &value
}
