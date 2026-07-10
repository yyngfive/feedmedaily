package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"strings"
	"time"
)

func (s *Store) LatestClassification(paperID int64) (*Classification, error) {
	return s.latestClassification(paperID)
}

func (s *Store) MarkPaperRead(paperID int64, now time.Time) (time.Time, error) {
	readAt, err := s.SetPaperRead(paperID, true, now)
	if err != nil {
		return time.Time{}, err
	}
	if readAt == nil {
		return time.Time{}, fmt.Errorf("update paper read status: missing read_at")
	}
	return *readAt, nil
}

func (s *Store) SetPaperRead(paperID int64, read bool, now time.Time) (*time.Time, error) {
	var readAtValue any
	if read {
		readAtValue = now.UTC().Format(time.RFC3339Nano)
	}
	result, err := s.db.Exec(`
		UPDATE papers
		SET read_at = CASE WHEN ? THEN COALESCE(read_at, ?) ELSE NULL END
		WHERE id = ?
	`, read, readAtValue, paperID)
	if err != nil {
		return nil, fmt.Errorf("update paper read status: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("update paper read status: %w", err)
	}
	if rowsAffected == 0 {
		return nil, ErrPaperNotFound
	}
	var readAt sql.NullString
	if err := s.db.QueryRow(`SELECT read_at FROM papers WHERE id = ?`, paperID).Scan(&readAt); err != nil {
		return nil, fmt.Errorf("reload paper read status: %w", err)
	}
	if !readAt.Valid || strings.TrimSpace(readAt.String) == "" {
		return nil, nil
	}
	parsed, err := parseTime(readAt.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func (s *Store) latestClassification(paperID int64) (*Classification, error) {
	row := s.db.QueryRow(fmt.Sprintf(`
		SELECT relevance, confidence, reason, topic_tags_json,
			%s AS recommended_action, model, %s AS translated_title_zh
		FROM classifications
		WHERE paper_id = ?
		ORDER BY classified_at DESC, id DESC
		LIMIT 1
	`, s.columnExpr(s.classificationColumns, "recommended_action", quote("scan")), s.columnExpr(s.classificationColumns, "translated_title_zh", "NULL")), paperID)

	var relevance string
	var confidence float64
	var reason string
	var topicTagsJSON string
	var recommendedAction string
	var model string
	var translatedTitleZH sql.NullString
	err := row.Scan(&relevance, &confidence, &reason, &topicTagsJSON, &recommendedAction, &model, &translatedTitleZH)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query latest classification for paper %d: %w", paperID, err)
	}
	classification, err := decodeClassification(relevance, confidence, reason, topicTagsJSON, recommendedAction, model, translatedTitleZH)
	if err != nil {
		return nil, fmt.Errorf("parse classification topic tags for paper %d: %w", paperID, err)
	}
	return classification, nil
}

func (s *Store) paperExists(paperID int64) (bool, error) {
	var value int
	err := s.db.QueryRow(`SELECT 1 FROM papers WHERE id = ? LIMIT 1`, paperID).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query paper %d: %w", paperID, err)
	}
	return true, nil
}
