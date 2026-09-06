package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"strings"
	"time"
)

func (s *Store) ListFeedback() ([]FeedbackRecord, error) {
	if len(s.feedbackColumns) == 0 || len(s.paperColumns) == 0 {
		return []FeedbackRecord{}, nil
	}
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT f.id, f.paper_id, p.title AS paper_title,
			f.original_relevance, f.corrected_relevance,
			%s AS original_topic, %s AS corrected_topic, f.note,
			%s AS state, %s AS used_in_prompt, f.created_at
		FROM feedback f
		JOIN papers p ON p.id = f.paper_id
		ORDER BY f.created_at DESC, f.id DESC
	`,
		s.columnExpr(s.feedbackColumns, "original_topic", "NULL"),
		s.columnExpr(s.feedbackColumns, "corrected_topic", "NULL"),
		s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen)), s.columnExpr(s.feedbackColumns, "used_in_prompt", "0")))
	if err != nil {
		return nil, fmt.Errorf("query feedback: %w", err)
	}
	defer rows.Close()

	items := []FeedbackRecord{}
	for rows.Next() {
		record, err := scanFeedbackRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feedback: %w", err)
	}
	return items, nil
}

func (s *Store) ListOpenFeedbackContexts() ([]ProposalFeedbackContext, error) {
	if len(s.feedbackColumns) == 0 || len(s.paperColumns) == 0 {
		return []ProposalFeedbackContext{}, nil
	}
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT f.id, f.paper_id, p.title AS paper_title, p.journal, p.abstract,
			f.original_relevance, f.corrected_relevance,
			%s AS original_topic, %s AS corrected_topic, f.note
		FROM feedback f
		JOIN papers p ON p.id = f.paper_id
		WHERE %s = ?
		ORDER BY f.created_at DESC, f.id DESC
	`,
		s.columnExpr(s.feedbackColumns, "original_topic", "NULL"),
		s.columnExpr(s.feedbackColumns, "corrected_topic", "NULL"),
		s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen))), feedbackStateOpen)
	if err != nil {
		return nil, fmt.Errorf("query open feedback contexts: %w", err)
	}
	defer rows.Close()

	items := []ProposalFeedbackContext{}
	for rows.Next() {
		var item ProposalFeedbackContext
		var journal sql.NullString
		var abstract sql.NullString
		var note sql.NullString
		var originalTopic sql.NullString
		var correctedTopic sql.NullString
		if err := rows.Scan(
			&item.FeedbackID,
			&item.PaperID,
			&item.PaperTitle,
			&journal,
			&abstract,
			&item.OriginalRelevance,
			&item.CorrectedRelevance,
			&originalTopic,
			&correctedTopic,
			&note,
		); err != nil {
			return nil, fmt.Errorf("scan open feedback context: %w", err)
		}
		item.Journal = nullableString(journal)
		item.Abstract = nullableString(abstract)
		item.Note = nullableString(note)
		if originalTopic.Valid {
			item.OriginalTopic = originalTopic.String
		}
		if correctedTopic.Valid {
			value := correctedTopic.String
			item.CorrectedTopic = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open feedback contexts: %w", err)
	}
	return items, nil
}

func (s *Store) FeedbackByID(id int64) (*FeedbackRecord, error) {
	if len(s.feedbackColumns) == 0 || len(s.paperColumns) == 0 {
		return nil, nil
	}
	row := s.db.QueryRow(fmt.Sprintf(`
		SELECT f.id, f.paper_id, p.title AS paper_title,
			f.original_relevance, f.corrected_relevance,
			%s AS original_topic, %s AS corrected_topic, f.note,
			%s AS state, %s AS used_in_prompt, f.created_at
		FROM feedback f
		JOIN papers p ON p.id = f.paper_id
		WHERE f.id = ?
	`,
		s.columnExpr(s.feedbackColumns, "original_topic", "NULL"),
		s.columnExpr(s.feedbackColumns, "corrected_topic", "NULL"),
		s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen)), s.columnExpr(s.feedbackColumns, "used_in_prompt", "0")), id)
	record, err := scanFeedbackRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "sql: no rows in result set") {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (s *Store) CreateFeedback(paperID int64, correctedRelevance string, correctedTopic *string, note *string, now time.Time) (*FeedbackRecord, error) {
	if !isSupportedRelevance(correctedRelevance) {
		return nil, fmt.Errorf("unsupported relevance value: %s", correctedRelevance)
	}
	exists, err := s.paperExists(paperID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrPaperNotFound
	}
	classification, err := s.latestClassification(paperID)
	if err != nil {
		return nil, err
	}
	if classification == nil {
		return nil, ErrClassificationNotFound
	}
	// original_topic 记录纠正前的原始值（可能是哨兵 none 或真实 id）；
	// corrected_topic 为 nil 表示用户要求“无主题”。
	var originalTopic any
	if len(classification.TopicTags) > 0 {
		originalTopic = classification.TopicTags[0]
	}
	result, err := s.db.Exec(`
		INSERT INTO feedback (
			paper_id, original_relevance, corrected_relevance, original_topic, corrected_topic, note, state, used_in_prompt, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, paperID, classification.Relevance, correctedRelevance, originalTopic, correctedTopic, note, feedbackStateOpen, 0, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("insert feedback: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read feedback id: %w", err)
	}
	record, err := s.FeedbackByID(id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("feedback disappeared after insert")
	}
	return record, nil
}

func (s *Store) DeleteFeedback(id int64) error {
	result, err := s.db.Exec(`DELETE FROM feedback WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete feedback %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete feedback %d: %w", id, err)
	}
	if rowsAffected == 0 {
		return ErrFeedbackNotFound
	}
	return nil
}

// FeedbackByIDs 按 id 批量读取 feedback 记录（含已消费的）。apply-proposal 的
// 重分类在作业开始前就把关联 feedback 关闭，对账只能按显式 id 列表取回它们。
func (s *Store) FeedbackByIDs(ids []int64) ([]FeedbackRecord, error) {
	if len(ids) == 0 || len(s.feedbackColumns) == 0 || len(s.paperColumns) == 0 {
		return []FeedbackRecord{}, nil
	}
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT f.id, f.paper_id, p.title AS paper_title,
			f.original_relevance, f.corrected_relevance,
			%s AS original_topic, %s AS corrected_topic, f.note,
			%s AS state, %s AS used_in_prompt, f.created_at
		FROM feedback f
		JOIN papers p ON p.id = f.paper_id
		WHERE f.id IN (%s)
		ORDER BY f.created_at DESC, f.id DESC
	`,
		s.columnExpr(s.feedbackColumns, "original_topic", "NULL"),
		s.columnExpr(s.feedbackColumns, "corrected_topic", "NULL"),
		s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen)), s.columnExpr(s.feedbackColumns, "used_in_prompt", "0"),
		strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("query feedback by ids: %w", err)
	}
	defer rows.Close()

	items := []FeedbackRecord{}
	for rows.Next() {
		record, err := scanFeedbackRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feedback by ids: %w", err)
	}
	return items, nil
}

// OpenFeedbackForPapers 返回指定论文集合上仍处于 open 状态的 feedback 记录，
// 供重分类后的纠正对账使用。
func (s *Store) OpenFeedbackForPapers(paperIDs []int64) ([]FeedbackRecord, error) {
	if len(paperIDs) == 0 || len(s.feedbackColumns) == 0 || len(s.paperColumns) == 0 {
		return []FeedbackRecord{}, nil
	}
	placeholders := make([]string, 0, len(paperIDs))
	args := make([]any, 0, len(paperIDs)+1)
	args = append(args, feedbackStateOpen)
	for _, paperID := range paperIDs {
		placeholders = append(placeholders, "?")
		args = append(args, paperID)
	}
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT f.id, f.paper_id, p.title AS paper_title,
			f.original_relevance, f.corrected_relevance,
			%s AS original_topic, %s AS corrected_topic, f.note,
			%s AS state, %s AS used_in_prompt, f.created_at
		FROM feedback f
		JOIN papers p ON p.id = f.paper_id
		WHERE %s = ? AND f.paper_id IN (%s)
		ORDER BY f.created_at DESC, f.id DESC
	`,
		s.columnExpr(s.feedbackColumns, "original_topic", "NULL"),
		s.columnExpr(s.feedbackColumns, "corrected_topic", "NULL"),
		s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen)), s.columnExpr(s.feedbackColumns, "used_in_prompt", "0"),
		s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen)),
		strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("query open feedback for papers: %w", err)
	}
	defer rows.Close()

	items := []FeedbackRecord{}
	for rows.Next() {
		record, err := scanFeedbackRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open feedback for papers: %w", err)
	}
	return items, nil
}

func (s *Store) MarkFeedbackUsed(ids []int64) error {
	for _, id := range ids {
		if _, err := s.db.Exec(`
			UPDATE feedback
			SET used_in_prompt = 1, state = ?
			WHERE id = ?
		`, feedbackStateUsed, id); err != nil {
			return fmt.Errorf("mark feedback %d used: %w", id, err)
		}
	}
	return nil
}

func (s *Store) PaperIDsForFeedbackIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT DISTINCT paper_id
		FROM feedback
		WHERE id IN (%s)
		ORDER BY created_at DESC, id DESC
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("query paper ids for feedback ids: %w", err)
	}
	defer rows.Close()
	result := []int64{}
	for rows.Next() {
		var paperID int64
		if err := rows.Scan(&paperID); err != nil {
			return nil, fmt.Errorf("scan paper id for feedback ids: %w", err)
		}
		result = append(result, paperID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate paper ids for feedback ids: %w", err)
	}
	return result, nil
}

func scanFeedbackRecord(scanner interface{ Scan(dest ...any) error }) (FeedbackRecord, error) {
	var record FeedbackRecord
	var originalTopic sql.NullString
	var correctedTopic sql.NullString
	var note sql.NullString
	var usedInPrompt int64
	var createdAt string
	if err := scanner.Scan(&record.ID, &record.PaperID, &record.PaperTitle, &record.OriginalRelevance, &record.CorrectedRelevance, &originalTopic, &correctedTopic, &note, &record.State, &usedInPrompt, &createdAt); err != nil {
		return FeedbackRecord{}, fmt.Errorf("scan feedback row: %w", err)
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return FeedbackRecord{}, fmt.Errorf("parse feedback created_at: %w", err)
	}
	record.OriginalTopic = nullableString(originalTopic)
	record.CorrectedTopic = nullableString(correctedTopic)
	record.Note = nullableString(note)
	record.UsedInProfile = usedInPrompt != 0
	record.CreatedAt = parsed
	return record, nil
}

func decodeFeedbackStatus(correctedRelevance *string, correctedTopic sql.NullString, note sql.NullString, createdAt *string, state *string, usedInPrompt sql.NullInt64) (*FeedbackStatus, error) {
	if correctedRelevance == nil && !correctedTopic.Valid && !note.Valid && createdAt == nil && state == nil && !usedInPrompt.Valid {
		return nil, nil
	}
	status := &FeedbackStatus{
		HasFeedback:        true,
		CorrectedRelevance: correctedRelevance,
		CorrectedTopic:     nullableString(correctedTopic),
		Note:               nullableString(note),
		State:              state,
		UsedInProfile:      usedInPrompt.Valid && usedInPrompt.Int64 != 0,
	}
	if createdAt != nil && strings.TrimSpace(*createdAt) != "" {
		parsed, err := parseTime(*createdAt)
		if err != nil {
			return nil, err
		}
		status.LatestFeedbackAt = &parsed
	}
	return status, nil
}

func FeedbackIDsToInt64(ids []int) []int64 {
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		result = append(result, int64(id))
	}
	return result
}
