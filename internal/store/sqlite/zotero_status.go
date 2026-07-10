package sqlite

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"strings"
	"time"
)

func (s *Store) LatestZoteroStatus(paperID int64) (*ZoteroStatus, error) {
	return s.latestZoteroStatus(paperID)
}

func (s *Store) UpsertZoteroStatus(paperID int64, state string, itemKey *string, errorMessage *string, now time.Time) (*ZoteroStatus, error) {
	cleanState := strings.TrimSpace(strings.ToLower(state))
	if cleanState != zoteroStateSaved && cleanState != zoteroStateError {
		return nil, fmt.Errorf("unsupported zotero status state: %s", state)
	}
	var savedAt any
	if cleanState == zoteroStateSaved {
		savedAt = now.UTC().Format(time.RFC3339Nano)
	} else {
		savedAt = nil
	}
	_, err := s.db.Exec(`
		INSERT INTO zotero_saves (paper_id, state, item_key, error_message, attempted_at, saved_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(paper_id) DO UPDATE SET
			state = excluded.state,
			item_key = excluded.item_key,
			error_message = excluded.error_message,
			attempted_at = excluded.attempted_at,
			saved_at = excluded.saved_at
	`, paperID, cleanState, itemKey, errorMessage, now.UTC().Format(time.RFC3339Nano), savedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert zotero status: %w", err)
	}
	status, err := s.latestZoteroStatus(paperID)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, fmt.Errorf("zotero status disappeared after upsert")
	}
	return status, nil
}

func (s *Store) latestZoteroStatus(paperID int64) (*ZoteroStatus, error) {
	if len(s.zoteroColumns) == 0 {
		return nil, nil
	}
	row := s.db.QueryRow(fmt.Sprintf(`
		SELECT %s AS state, %s AS item_key, %s AS error_message,
			%s AS attempted_at, %s AS saved_at
		FROM zotero_saves
		WHERE paper_id = ?
	`, s.columnExpr(s.zoteroColumns, "state", "NULL"),
		s.columnExpr(s.zoteroColumns, "item_key", "NULL"),
		s.columnExpr(s.zoteroColumns, "error_message", "NULL"),
		s.columnExpr(s.zoteroColumns, "attempted_at", "NULL"),
		s.columnExpr(s.zoteroColumns, "saved_at", "NULL")), paperID)

	var state sql.NullString
	var itemKey sql.NullString
	var errorMessage sql.NullString
	var attemptedAt sql.NullString
	var savedAt sql.NullString
	err := row.Scan(&state, &itemKey, &errorMessage, &attemptedAt, &savedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query zotero status for paper %d: %w", paperID, err)
	}
	status, err := decodeZoteroStatus(state, itemKey, errorMessage, attemptedAt, savedAt)
	if err != nil {
		return nil, fmt.Errorf("parse zotero status for paper %d: %w", paperID, err)
	}
	return status, nil
}

func decodeZoteroStatus(state sql.NullString, itemKey sql.NullString, errorMessage sql.NullString, attemptedAt sql.NullString, savedAt sql.NullString) (*ZoteroStatus, error) {
	if !state.Valid && !itemKey.Valid && !errorMessage.Valid && !attemptedAt.Valid && !savedAt.Valid {
		return nil, nil
	}
	status := &ZoteroStatus{
		State:     nullableString(state),
		Saved:     state.Valid && state.String == zoteroStateSaved,
		ItemKey:   nullableString(itemKey),
		LastError: nullableString(errorMessage),
	}
	if attemptedAt.Valid {
		parsed, err := parseTime(attemptedAt.String)
		if err != nil {
			return nil, err
		}
		status.AttemptedAt = &parsed
	}
	if savedAt.Valid {
		parsed, err := parseTime(savedAt.String)
		if err != nil {
			return nil, err
		}
		status.SavedAt = &parsed
	}
	return status, nil
}
