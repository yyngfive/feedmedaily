package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	abstractSourceRSS      = "rss"
	abstractSourceCrossref = "crossref"
	abstractSourceOpenAlex = "openalex"
)

type Paper struct {
	ID             int64
	SourceURL      string
	FeedTitle      *string
	Title          string
	URL            string
	DOI            *string
	Journal        *string
	Authors        []string
	Abstract       *string
	AbstractHTML   *string
	AbstractImages []AbstractImage
	AbstractSource string
	PublishedDate  *string
	FirstSeenAt    time.Time
	ReadAt         *time.Time
	Raw            map[string]any
}

var doiPattern = regexp.MustCompile(`10\.\d{4,9}/[-._;()/:A-Z0-9]+`)

func (s *Store) PaperByID(paperID int64) (*Paper, error) {
	if len(s.paperColumns) == 0 {
		return nil, nil
	}
	row := s.db.QueryRow(fmt.Sprintf(`
		SELECT id, source_url, feed_title, title, url, doi, journal, authors_json, abstract,
			%s AS abstract_source, published_date, first_seen_at, %s AS read_at, raw_json
		FROM papers
		WHERE id = ?
	`, s.columnExpr(s.paperColumns, "abstract_source", quote(abstractSourceNone)), s.columnExpr(s.paperColumns, "read_at", "NULL")), paperID)
	base, err := scanPaperRow(row)
	if err != nil {
		if strings.Contains(err.Error(), "sql: no rows in result set") {
			return nil, nil
		}
		return nil, err
	}
	paper, err := paperFromPaperRow(base)
	if err != nil {
		return nil, err
	}
	return &paper, nil
}

func (s *Store) UpsertPaper(paper Paper, now time.Time) (int64, bool, error) {
	return s.UpsertPaperWithKey(paper, paperKey(paper), now)
}

// UpsertPaperWithKey 以调用方给定的稳定键 upsert。enrichment 写回会补上 DOI，
// 若按内容重新算键会把同一篇文章拆成多行，因此写回路径必须沿用原行已存储的键。
func (s *Store) UpsertPaperWithKey(paper Paper, key string, now time.Time) (int64, bool, error) {
	if strings.TrimSpace(key) == "" {
		return 0, false, fmt.Errorf("paper key cannot be blank")
	}
	existing, err := s.findPaperByKey(key)
	if err != nil {
		return 0, false, err
	}
	if existing != nil {
		merged := mergePaperContent(*existing, paper)
		rawJSON, authorsJSON, err := encodeStoredPaper(merged)
		if err != nil {
			return 0, false, err
		}
		assignments := []string{
			"doi = COALESCE(?, doi)",
			"title = ?",
			"journal = COALESCE(?, journal)",
			"url = ?",
			"source_url = ?",
			"feed_title = ?",
			"authors_json = ?",
			"abstract = ?",
			"abstract_source = ?",
			"published_date = COALESCE(?, published_date)",
			"raw_json = ?",
		}
		args := []any{merged.DOI, merged.Title, merged.Journal, merged.URL, merged.SourceURL, merged.FeedTitle, authorsJSON, merged.Abstract, merged.AbstractSource, merged.PublishedDate, rawJSON}
		if s.paperColumns["paper_key"] {
			assignments = append(assignments, "paper_key = ?")
			args = append(args, key)
		}
		if s.paperColumns["last_checked_at"] {
			assignments = append(assignments, "last_checked_at = ?")
			args = append(args, now.UTC().Format(time.RFC3339Nano))
		}
		args = append(args, existing.ID)
		_, err = s.db.Exec(fmt.Sprintf(`
			UPDATE papers
			SET %s
			WHERE id = ?
		`, strings.Join(assignments, ", ")), args...)
		if err != nil {
			return 0, false, fmt.Errorf("update paper: %w", err)
		}
		return existing.ID, false, nil
	}
	rawJSON, authorsJSON, err := encodeStoredPaper(paper)
	if err != nil {
		return 0, false, err
	}
	firstSeenAt := paper.FirstSeenAt
	if firstSeenAt.IsZero() {
		firstSeenAt = now.UTC()
	}
	columns := []string{
		"source_url", "feed_title", "title", "url", "doi", "journal", "authors_json", "abstract",
		"abstract_source", "published_date", "first_seen_at", "read_at", "raw_json",
	}
	args := []any{
		paper.SourceURL, paper.FeedTitle, paper.Title, paper.URL, paper.DOI, paper.Journal, authorsJSON, paper.Abstract,
		normalizeAbstractSource(paper.AbstractSource), paper.PublishedDate, firstSeenAt.Format(time.RFC3339Nano), formatNullableTime(paper.ReadAt), rawJSON,
	}
	if s.paperColumns["paper_key"] {
		columns = append([]string{"paper_key"}, columns...)
		args = append([]any{key}, args...)
	}
	if s.paperColumns["last_checked_at"] {
		insertAt := firstSeenAt.Format(time.RFC3339Nano)
		index := slices.Index(columns, "raw_json")
		columns = append(columns[:index], append([]string{"last_checked_at"}, columns[index:]...)...)
		args = append(args[:index], append([]any{insertAt}, args[index:]...)...)
	}
	placeholders := make([]string, 0, len(columns))
	for range columns {
		placeholders = append(placeholders, "?")
	}
	result, err := s.db.Exec(fmt.Sprintf(`
		INSERT INTO papers (%s)
		VALUES (%s)
	`, strings.Join(columns, ", "), strings.Join(placeholders, ", ")), args...)
	if err != nil {
		return 0, false, fmt.Errorf("insert paper: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("read paper id: %w", err)
	}
	return id, true, nil
}

func (s *Store) SaveClassification(paperID int64, classification Classification, now time.Time) error {
	topicTagsJSON, err := json.Marshal(classification.TopicTags)
	if err != nil {
		return fmt.Errorf("encode classification topic tags: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO classifications (
			paper_id, relevance, confidence, reason, topic_tags_json,
			recommended_action, model, translated_title_zh, classified_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, paperID, classification.Relevance, classification.Confidence, classification.Reason, string(topicTagsJSON), defaultRecommendedAction(classification.RecommendedAction), classification.Model, classification.TranslatedTitleZH, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert classification: %w", err)
	}
	return nil
}

func (s *Store) PaperIDsNeedingClassification(paperIDs []int64) ([]int64, error) {
	pending := make([]int64, 0, len(paperIDs))
	for _, paperID := range paperIDs {
		classification, err := s.latestClassification(paperID)
		if err != nil {
			return nil, err
		}
		if classification == nil {
			pending = append(pending, paperID)
		}
	}
	return pending, nil
}

func (s *Store) RecentPaperIDs(limit int) ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM papers ORDER BY first_seen_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent paper ids: %w", err)
	}
	defer rows.Close()
	return scanIDRows(rows, "recent paper ids")
}

func (s *Store) PaperIDsSeenBetween(start time.Time, end time.Time) ([]int64, error) {
	rows, err := s.db.Query(`
		SELECT id FROM papers
		WHERE first_seen_at >= ? AND first_seen_at < ?
		ORDER BY first_seen_at DESC
	`, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("query paper ids by first-seen time: %w", err)
	}
	defer rows.Close()
	return scanIDRows(rows, "paper ids by first-seen time")
}

func (s *Store) PaperCountsSeenBetween(start time.Time, end time.Time) (int, int, error) {
	var total int
	var classified int
	err := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN EXISTS (
				SELECT 1 FROM classifications c WHERE c.paper_id = p.id
			) THEN 1 ELSE 0 END), 0)
		FROM papers p
		WHERE first_seen_at >= ? AND first_seen_at < ?
	`, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano)).Scan(&total, &classified)
	if err != nil {
		return 0, 0, fmt.Errorf("count papers by first-seen time: %w", err)
	}
	return total, classified, nil
}

func (s *Store) PaperCount() (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM papers`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count papers: %w", err)
	}
	return count, nil
}

func (s *Store) ClassifiedPaperCount() (int, error) {
	var count int
	if err := s.db.QueryRow(`
		SELECT COUNT(*) FROM papers p
		WHERE EXISTS (SELECT 1 FROM classifications c WHERE c.paper_id = p.id)
	`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count classified papers: %w", err)
	}
	return count, nil
}

func (s *Store) RecentPaperClassificationCounts(limit int) (int, int, error) {
	var total int
	var classified int
	err := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN EXISTS (
				SELECT 1 FROM classifications c WHERE c.paper_id = selected.id
			) THEN 1 ELSE 0 END), 0)
		FROM (
			SELECT id FROM papers ORDER BY first_seen_at DESC LIMIT ?
		) selected
	`, limit).Scan(&total, &classified)
	if err != nil {
		return 0, 0, fmt.Errorf("count recent paper classifications: %w", err)
	}
	return total, classified, nil
}

func (s *Store) AllPaperIDs() ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM papers ORDER BY first_seen_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query all paper ids: %w", err)
	}
	defer rows.Close()
	return scanIDRows(rows, "all paper ids")
}

func (s *Store) UnclassifiedPaperIDs() ([]int64, error) {
	rows, err := s.db.Query(`
		SELECT p.id FROM papers p
		WHERE NOT EXISTS (SELECT 1 FROM classifications c WHERE c.paper_id = p.id)
		ORDER BY p.first_seen_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query unclassified paper ids: %w", err)
	}
	defer rows.Close()
	return scanIDRows(rows, "unclassified paper ids")
}

func (s *Store) FeedbackPaperIDs() ([]int64, error) {
	rows, err := s.db.Query(`SELECT DISTINCT paper_id FROM feedback ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query feedback paper ids: %w", err)
	}
	defer rows.Close()
	return scanIDRows(rows, "feedback paper ids")
}

func paperFromPaperRow(base paperRow) (Paper, error) {
	authors := []string{}
	if strings.TrimSpace(base.AuthorsJSON) != "" {
		if err := json.Unmarshal([]byte(base.AuthorsJSON), &authors); err != nil {
			return Paper{}, fmt.Errorf("parse paper authors: %w", err)
		}
	}
	raw, abstractHTML, abstractImages, err := parseRawPayload(base.RawJSON)
	if err != nil {
		return Paper{}, fmt.Errorf("parse paper raw payload: %w", err)
	}
	return Paper{
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
		AbstractSource: normalizeAbstractSource(base.AbstractSource),
		PublishedDate:  base.PublishedDate,
		FirstSeenAt:    base.FirstSeenAt,
		ReadAt:         base.ReadAt,
		Raw:            raw,
	}, nil
}

func encodeStoredPaper(paper Paper) (string, string, error) {
	raw := map[string]any{}
	for key, value := range paper.Raw {
		raw[key] = value
	}
	raw["_abstract_html"] = paper.AbstractHTML
	raw["_abstract_images"] = paper.AbstractImages
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		return "", "", fmt.Errorf("encode paper raw payload: %w", err)
	}
	authorsJSON, err := json.Marshal(paper.Authors)
	if err != nil {
		return "", "", fmt.Errorf("encode paper authors: %w", err)
	}
	return string(rawJSON), string(authorsJSON), nil
}

func mergePaperContent(existing Paper, incoming Paper) Paper {
	chosenAbstract, chosenSource, chosenHTML, chosenImages := pickAbstractPayload(existing, incoming)
	mergedRaw := map[string]any{}
	for key, value := range existing.Raw {
		mergedRaw[key] = value
	}
	for key, value := range incoming.Raw {
		mergedRaw[key] = value
	}
	result := existing
	result.SourceURL = firstNonEmpty(incoming.SourceURL, existing.SourceURL)
	result.FeedTitle = firstNonNilString(incoming.FeedTitle, existing.FeedTitle)
	result.Title = firstNonEmpty(incoming.Title, existing.Title)
	result.URL = firstNonEmpty(incoming.URL, existing.URL)
	result.DOI = firstNonNilString(incoming.DOI, existing.DOI)
	result.Journal = firstNonNilString(incoming.Journal, existing.Journal)
	if len(incoming.Authors) > 0 {
		result.Authors = append([]string{}, incoming.Authors...)
	}
	result.Abstract = chosenAbstract
	result.AbstractSource = chosenSource
	result.AbstractHTML = chosenHTML
	result.AbstractImages = chosenImages
	result.PublishedDate = firstNonNilString(incoming.PublishedDate, existing.PublishedDate)
	result.ReadAt = existing.ReadAt
	result.Raw = mergedRaw
	return result
}

func pickAbstractPayload(existing Paper, incoming Paper) (*string, string, *string, []AbstractImage) {
	existingHasContent := paperHasAbstractContent(existing)
	incomingHasContent := paperHasAbstractContent(incoming)
	if incomingHasContent && (!existingHasContent || abstractSourcePriority(incoming.AbstractSource) >= abstractSourcePriority(existing.AbstractSource)) {
		return incoming.Abstract, normalizeAbstractSource(incoming.AbstractSource), incoming.AbstractHTML, incoming.AbstractImages
	}
	if existingHasContent {
		return existing.Abstract, normalizeAbstractSource(existing.AbstractSource), existing.AbstractHTML, existing.AbstractImages
	}
	return firstNonNilString(incoming.Abstract, existing.Abstract), abstractSourceNone, nil, []AbstractImage{}
}

func abstractSourcePriority(source string) int {
	switch normalizeAbstractSource(source) {
	case abstractSourceRSS:
		return 1
	case abstractSourceCrossref:
		return 2
	case abstractSourceOpenAlex:
		return 3
	default:
		return 0
	}
}

func paperHasAbstractContent(paper Paper) bool {
	return paper.Abstract != nil || paper.AbstractHTML != nil || len(paper.AbstractImages) > 0
}

func normalizeAbstractSource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case abstractSourceRSS:
		return abstractSourceRSS
	case abstractSourceCrossref:
		return abstractSourceCrossref
	case abstractSourceOpenAlex:
		return abstractSourceOpenAlex
	default:
		return abstractSourceNone
	}
}

func defaultRecommendedAction(value string) string {
	clean := strings.TrimSpace(strings.ToLower(value))
	switch clean {
	case "read", "scan", "skip":
		return clean
	default:
		return "scan"
	}
}

func scanIDRows(rows *sql.Rows, label string) ([]int64, error) {
	result := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return result, nil
}

func formatNullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func firstNonEmpty(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func firstNonNilString(primary *string, fallback *string) *string {
	if primary != nil && strings.TrimSpace(*primary) != "" {
		value := *primary
		return &value
	}
	if fallback == nil {
		return nil
	}
	value := *fallback
	return &value
}

func paperKey(paper Paper) string {
	doi := normalizeDOI(stringValue(paper.DOI))
	if doi != "" {
		return "doi:" + doi
	}
	if strings.TrimSpace(paper.URL) != "" {
		return "url:" + strings.ToLower(strings.TrimSpace(paper.URL))
	}
	return "title:" + strings.ToLower(strings.TrimSpace(paper.Title))
}

func normalizeDOI(value string) string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return ""
	}
	match := doiPattern.FindString(strings.ToUpper(clean))
	if match == "" {
		return strings.TrimPrefix(strings.ToLower(clean), "doi:")
	}
	return strings.TrimSuffix(strings.ToLower(match), ".")
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// StoredPaperKey 返回库中已存储的 paper_key，enrichment 写回时用它保持键稳定。
// schema 没有 paper_key 列或行不存在时返回空串，调用方退回内容重算键。
func (s *Store) StoredPaperKey(paperID int64) (string, error) {
	if !s.paperColumns["paper_key"] {
		return "", nil
	}
	var key string
	err := s.db.QueryRow(`SELECT paper_key FROM papers WHERE id = ?`, paperID).Scan(&key)
	if err != nil {
		if strings.Contains(err.Error(), "sql: no rows in result set") {
			return "", nil
		}
		return "", fmt.Errorf("query stored paper key: %w", err)
	}
	return key, nil
}

// ClearPaperDOI 清除指定论文的 DOI。enrichment 校验判定 DOI 与标题、日期都
// 对不上时调用，链接回退到出版社 URL。
func (s *Store) ClearPaperDOI(paperID int64) error {
	if _, err := s.db.Exec(`UPDATE papers SET doi = NULL WHERE id = ?`, paperID); err != nil {
		return fmt.Errorf("clear paper doi: %w", err)
	}
	return nil
}

func (s *Store) findPaperByKey(key string) (*Paper, error) {
	if s.paperColumns["paper_key"] {
		row := s.db.QueryRow(fmt.Sprintf(`
			SELECT id, source_url, feed_title, title, url, doi, journal, authors_json, abstract,
				%s AS abstract_source, published_date, first_seen_at, %s AS read_at, raw_json
			FROM papers
			WHERE paper_key = ?
			ORDER BY id DESC
		`, s.columnExpr(s.paperColumns, "abstract_source", quote(abstractSourceNone)), s.columnExpr(s.paperColumns, "read_at", "NULL")), key)
		base, err := scanPaperRow(row)
		if err != nil {
			if strings.Contains(err.Error(), "sql: no rows in result set") {
				return nil, nil
			}
			return nil, fmt.Errorf("query paper by key: %w", err)
		}
		paper, err := paperFromPaperRow(base)
		if err != nil {
			return nil, err
		}
		return &paper, nil
	}
	// 旧 schema 没有存储 paper_key 列：按内容重算键后逐行比对。
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT id, source_url, feed_title, title, url, doi, journal, authors_json, abstract,
			%s AS abstract_source, published_date, first_seen_at, %s AS read_at, raw_json
		FROM papers
		ORDER BY id DESC
	`, s.columnExpr(s.paperColumns, "abstract_source", quote(abstractSourceNone)), s.columnExpr(s.paperColumns, "read_at", "NULL")))
	if err != nil {
		return nil, fmt.Errorf("query papers for key lookup: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		base, err := scanPaperRow(rows)
		if err != nil {
			return nil, err
		}
		paper, err := paperFromPaperRow(base)
		if err != nil {
			return nil, err
		}
		if paperKey(paper) == key {
			return &paper, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate papers for key lookup: %w", err)
	}
	return nil, nil
}
