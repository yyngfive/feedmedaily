package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"strings"
	"time"
)

func (s *Store) BuildLatestReport(now time.Time) (Report, error) {
	// 从 papers + latest classification + feedback + zotero 实时组装最新报告。
	report := emptyReport(now.UTC())
	if len(s.paperColumns) == 0 || len(s.classificationColumns) == 0 {
		return report, nil
	}
	lastUpdatedAt, err := s.latestReportUpdatedAt()
	if err != nil {
		return Report{}, err
	}
	papers, err := s.listReportPapers()
	if err != nil {
		return Report{}, err
	}
	report.LastUpdatedAt = lastUpdatedAt
	report.Papers = papers
	report.Totals = reportTotals(papers)
	return report, nil
}

func (s *Store) latestReportUpdatedAt() (*time.Time, error) {
	if len(s.paperColumns) == 0 {
		return nil, nil
	}
	column := s.columnExpr(s.paperColumns, "last_checked_at", "first_seen_at")
	var raw sql.NullString
	if err := s.db.QueryRow(fmt.Sprintf(`SELECT MAX(%s) FROM papers`, column)).Scan(&raw); err != nil {
		return nil, fmt.Errorf("query latest report updated at: %w", err)
	}
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return nil, nil
	}
	parsed, err := parseTime(raw.String)
	if err != nil {
		return nil, fmt.Errorf("parse latest report updated at: %w", err)
	}
	return &parsed, nil
}

func (s *Store) listReportPapers() ([]ReportPaper, error) {
	rows, err := s.db.Query(s.reportPapersQuery())
	if err != nil {
		return nil, fmt.Errorf("query report papers: %w", err)
	}
	defer rows.Close()

	items := []ReportPaper{}
	for rows.Next() {
		paper, err := scanReportPaper(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, paper)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate report papers: %w", err)
	}
	return items, nil
}

func (s *Store) reportPapersQuery() string {
	latestFeedbackCTE := `
	latest_feedback AS (
		SELECT NULL AS paper_id, NULL AS corrected_relevance, NULL AS note,
			NULL AS created_at, NULL AS state, NULL AS used_in_prompt, NULL AS rn
		WHERE 0
	)`
	if len(s.feedbackColumns) > 0 {
		latestFeedbackCTE = fmt.Sprintf(`
	latest_feedback AS (
		SELECT paper_id, corrected_relevance, note, created_at,
			%s AS state, %s AS used_in_prompt,
			ROW_NUMBER() OVER (PARTITION BY paper_id ORDER BY created_at DESC, id DESC) AS rn
		FROM feedback
		WHERE %s = %s
	)`,
			s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen)),
			s.columnExpr(s.feedbackColumns, "used_in_prompt", "0"),
			s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen)),
			quote(feedbackStateOpen),
		)
	}

	latestZoteroCTE := `
	latest_zotero AS (
		SELECT NULL AS paper_id, NULL AS state, NULL AS item_key, NULL AS error_message,
			NULL AS attempted_at, NULL AS saved_at, NULL AS rn
		WHERE 0
	)`
	if len(s.zoteroColumns) > 0 {
		latestZoteroCTE = fmt.Sprintf(`
	latest_zotero AS (
		SELECT paper_id, %s AS state, %s AS item_key, %s AS error_message,
			%s AS attempted_at, %s AS saved_at,
			ROW_NUMBER() OVER (
				PARTITION BY paper_id
				ORDER BY COALESCE(%s, ''), id DESC
			) AS rn
		FROM zotero_saves
	)`,
			s.columnExpr(s.zoteroColumns, "state", "NULL"),
			s.columnExpr(s.zoteroColumns, "item_key", "NULL"),
			s.columnExpr(s.zoteroColumns, "error_message", "NULL"),
			s.columnExpr(s.zoteroColumns, "attempted_at", "NULL"),
			s.columnExpr(s.zoteroColumns, "saved_at", "NULL"),
			s.columnExpr(s.zoteroColumns, "attempted_at", "NULL"),
		)
	}

	return fmt.Sprintf(`
		WITH latest_classifications AS (
			SELECT paper_id, relevance, confidence, reason, topic_tags_json,
				%s AS recommended_action, model, %s AS translated_title_zh,
				ROW_NUMBER() OVER (
					PARTITION BY paper_id
					ORDER BY classified_at DESC, id DESC
				) AS rn
			FROM classifications
		),
		%s,
		%s
		SELECT
			p.id, p.source_url, p.feed_title, p.title, p.url, p.doi, p.journal, p.authors_json, p.abstract,
			%s AS abstract_source, p.published_date, p.first_seen_at, %s AS read_at, p.raw_json,
			lc.relevance, lc.confidence, lc.reason, lc.topic_tags_json, lc.recommended_action, lc.model, lc.translated_title_zh,
			lf.corrected_relevance, lf.note, lf.created_at, lf.state, lf.used_in_prompt,
			lz.state, lz.item_key, lz.error_message, lz.attempted_at, lz.saved_at
		FROM papers p
		JOIN latest_classifications lc ON lc.paper_id = p.id AND lc.rn = 1
		LEFT JOIN latest_feedback lf ON lf.paper_id = p.id AND lf.rn = 1
		LEFT JOIN latest_zotero lz ON lz.paper_id = p.id AND lz.rn = 1
		ORDER BY p.first_seen_at DESC
	`,
		s.columnExpr(s.classificationColumns, "recommended_action", quote("scan")),
		s.columnExpr(s.classificationColumns, "translated_title_zh", "NULL"),
		latestFeedbackCTE,
		latestZoteroCTE,
		s.columnExpr(s.paperColumns, "abstract_source", quote(abstractSourceNone)),
		s.columnExpr(s.paperColumns, "read_at", "NULL"),
	)
}

func (s *Store) latestFeedbackStatus(paperID int64) (*FeedbackStatus, error) {
	if len(s.feedbackColumns) == 0 {
		return nil, nil
	}
	row := s.db.QueryRow(fmt.Sprintf(`
		SELECT corrected_relevance, note, created_at, %s AS state, %s AS used_in_prompt
		FROM feedback
		WHERE paper_id = ? AND %s = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen)),
		s.columnExpr(s.feedbackColumns, "used_in_prompt", "0"),
		s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen))), paperID, feedbackStateOpen)

	var correctedRelevance string
	var note sql.NullString
	var createdAt string
	var state string
	var usedInPrompt int64
	err := row.Scan(&correctedRelevance, &note, &createdAt, &state, &usedInPrompt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query latest feedback for paper %d: %w", paperID, err)
	}
	status, err := decodeFeedbackStatus(
		stringPtr(correctedRelevance),
		note,
		stringPtr(createdAt),
		stringPtr(state),
		sql.NullInt64{Int64: usedInPrompt, Valid: true},
	)
	if err != nil {
		return nil, fmt.Errorf("parse feedback timestamp for paper %d: %w", paperID, err)
	}
	return status, nil
}

func buildReportPaper(base paperRow, classification Classification, feedbackStatus *FeedbackStatus, zoteroStatus *ZoteroStatus) (ReportPaper, error) {
	authors := []string{}
	if strings.TrimSpace(base.AuthorsJSON) != "" {
		if err := json.Unmarshal([]byte(base.AuthorsJSON), &authors); err != nil {
			return ReportPaper{}, fmt.Errorf("parse authors for paper %d: %w", base.ID, err)
		}
	}
	abstractHTML, abstractImages, err := parseReportRawPayload(base.RawJSON)
	if err != nil {
		return ReportPaper{}, fmt.Errorf("parse raw payload for paper %d: %w", base.ID, err)
	}
	return ReportPaper{
		ID:             base.ID,
		SourceURL:      base.SourceURL,
		FeedTitle:      base.FeedTitle,
		Title:          base.Title,
		URL:            base.URL,
		DOI:            base.DOI,
		Journal:        base.Journal,
		Authors:        authors,
		Abstract:       base.Abstract,
		AbstractHTML:   abstractHTML,
		AbstractImages: abstractImages,
		AbstractSource: base.AbstractSource,
		PublishedDate:  base.PublishedDate,
		FirstSeenAt:    base.FirstSeenAt,
		ReadAt:         base.ReadAt,
		Raw:            map[string]any{},
		Classification: classification,
		SeenDate:       base.FirstSeenAt.Format("2006-01-02"),
		FeedbackStatus: feedbackStatus,
		ZoteroStatus:   zoteroStatus,
	}, nil
}

func scanReportPaper(scanner interface{ Scan(dest ...any) error }) (ReportPaper, error) {
	var base paperRow
	var feedTitle sql.NullString
	var doi sql.NullString
	var journal sql.NullString
	var abstract sql.NullString
	var publishedDate sql.NullString
	var firstSeenAt string
	var readAt sql.NullString

	var relevance string
	var confidence float64
	var reason string
	var topicTagsJSON string
	var recommendedAction string
	var model string
	var translatedTitleZH sql.NullString

	var correctedRelevance sql.NullString
	var feedbackNote sql.NullString
	var feedbackCreatedAt sql.NullString
	var feedbackState sql.NullString
	var feedbackUsedInPrompt sql.NullInt64

	var zoteroState sql.NullString
	var zoteroItemKey sql.NullString
	var zoteroErrorMessage sql.NullString
	var zoteroAttemptedAt sql.NullString
	var zoteroSavedAt sql.NullString

	if err := scanner.Scan(
		&base.ID,
		&base.SourceURL,
		&feedTitle,
		&base.Title,
		&base.URL,
		&doi,
		&journal,
		&base.AuthorsJSON,
		&abstract,
		&base.AbstractSource,
		&publishedDate,
		&firstSeenAt,
		&readAt,
		&base.RawJSON,
		&relevance,
		&confidence,
		&reason,
		&topicTagsJSON,
		&recommendedAction,
		&model,
		&translatedTitleZH,
		&correctedRelevance,
		&feedbackNote,
		&feedbackCreatedAt,
		&feedbackState,
		&feedbackUsedInPrompt,
		&zoteroState,
		&zoteroItemKey,
		&zoteroErrorMessage,
		&zoteroAttemptedAt,
		&zoteroSavedAt,
	); err != nil {
		return ReportPaper{}, fmt.Errorf("scan report paper row: %w", err)
	}

	parsedFirstSeenAt, err := parseTime(firstSeenAt)
	if err != nil {
		return ReportPaper{}, fmt.Errorf("parse paper first_seen_at: %w", err)
	}
	base.FeedTitle = nullableString(feedTitle)
	base.DOI = nullableString(doi)
	base.Journal = nullableString(journal)
	base.Abstract = nullableString(abstract)
	base.PublishedDate = nullableString(publishedDate)
	base.FirstSeenAt = parsedFirstSeenAt
	if base.AbstractSource == "" {
		base.AbstractSource = abstractSourceNone
	}
	if readAt.Valid {
		parsedReadAt, err := parseTime(readAt.String)
		if err != nil {
			return ReportPaper{}, fmt.Errorf("parse paper read_at: %w", err)
		}
		base.ReadAt = &parsedReadAt
	}

	classification, err := decodeClassification(relevance, confidence, reason, topicTagsJSON, recommendedAction, model, translatedTitleZH)
	if err != nil {
		return ReportPaper{}, fmt.Errorf("parse report classification for paper %d: %w", base.ID, err)
	}
	feedbackStatus, err := decodeFeedbackStatus(nullableString(correctedRelevance), feedbackNote, nullableString(feedbackCreatedAt), nullableString(feedbackState), feedbackUsedInPrompt)
	if err != nil {
		return ReportPaper{}, fmt.Errorf("parse report feedback status for paper %d: %w", base.ID, err)
	}
	zoteroStatus, err := decodeZoteroStatus(zoteroState, zoteroItemKey, zoteroErrorMessage, zoteroAttemptedAt, zoteroSavedAt)
	if err != nil {
		return ReportPaper{}, fmt.Errorf("parse report zotero status for paper %d: %w", base.ID, err)
	}
	return buildReportPaper(base, *classification, feedbackStatus, zoteroStatus)
}

func parseReportRawPayload(rawJSON string) (*string, []AbstractImage, error) {
	if strings.TrimSpace(rawJSON) == "" {
		return nil, []AbstractImage{}, nil
	}
	var payload struct {
		AbstractHTML   *string         `json:"_abstract_html"`
		AbstractImages []AbstractImage `json:"_abstract_images"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		return nil, nil, err
	}
	if payload.AbstractImages == nil {
		payload.AbstractImages = []AbstractImage{}
	}
	return payload.AbstractHTML, payload.AbstractImages, nil
}

func reportTotals(papers []ReportPaper) map[string]int {
	totals := map[string]int{
		"total":     len(papers),
		"direct":    0,
		"indirect":  0,
		"unrelated": 0,
	}
	for _, paper := range papers {
		if _, ok := totals[paper.Classification.Relevance]; ok {
			totals[paper.Classification.Relevance]++
		}
	}
	return totals
}

func emptyReport(now time.Time) Report {
	return Report{
		GeneratedAt:   now,
		LastUpdatedAt: nil,
		ReportDate:    now.Format("2006-01-02"),
		Totals: map[string]int{
			"total":     0,
			"direct":    0,
			"indirect":  0,
			"unrelated": 0,
		},
		Papers: []ReportPaper{},
		Errors: []string{},
	}
}
