package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/profile"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	feedbackStateOpen     = "open"
	feedbackStateUsed     = "used"
	proposalStateApplied  = "applied"
	proposalStateRejected = "rejected"
	proposalStatePending  = "pending"
	zoteroStateError      = "error"
	zoteroStateSaved      = "saved"
	abstractSourceNone    = "none"
	sqliteBusyTimeoutMS   = 5000
	readStoreMaxOpenConns = 4
	writeStoreMaxOpenConn = 1
)

var (
	ErrPaperNotFound           = errors.New("paper not found")
	ErrFeedbackNotFound        = errors.New("feedback not found")
	ErrProfileProposalNotFound = errors.New("profile proposal not found")
	ErrClassificationNotFound  = errors.New("paper has no classification yet")
)

type Store struct {
	db                    *sql.DB
	paperColumns          map[string]bool
	classificationColumns map[string]bool
	feedbackColumns       map[string]bool
	proposalColumns       map[string]bool
	zoteroColumns         map[string]bool
}

type storePoolRole int

const (
	storePoolRoleWrite storePoolRole = iota
	storePoolRoleRead
)

type AbstractImage struct {
	Src string  `json:"src"`
	Alt *string `json:"alt"`
}

type Classification struct {
	Relevance         string   `json:"relevance"`
	Confidence        float64  `json:"confidence"`
	TopicTags         []string `json:"topic_tags"`
	Reason            string   `json:"reason"`
	RecommendedAction string   `json:"recommended_action"`
	Model             string   `json:"model"`
	TranslatedTitleZH *string  `json:"translated_title_zh"`
}

type FeedbackStatus struct {
	HasFeedback        bool       `json:"has_feedback"`
	CorrectedRelevance *string    `json:"corrected_relevance"`
	Note               *string    `json:"note"`
	LatestFeedbackAt   *time.Time `json:"latest_feedback_at"`
	State              *string    `json:"state"`
	UsedInProfile      bool       `json:"used_in_profile"`
}

type ZoteroStatus struct {
	State       *string    `json:"state"`
	Saved       bool       `json:"saved"`
	ItemKey     *string    `json:"item_key"`
	LastError   *string    `json:"last_error"`
	AttemptedAt *time.Time `json:"attempted_at"`
	SavedAt     *time.Time `json:"saved_at"`
}

type ReportPaper struct {
	ID             int64           `json:"id"`
	SourceURL      string          `json:"source_url"`
	FeedTitle      *string         `json:"feed_title"`
	Title          string          `json:"title"`
	URL            string          `json:"url"`
	DOI            *string         `json:"doi"`
	Journal        *string         `json:"journal"`
	Authors        []string        `json:"authors"`
	Abstract       *string         `json:"abstract"`
	AbstractHTML   *string         `json:"abstract_html"`
	AbstractImages []AbstractImage `json:"abstract_images"`
	AbstractSource string          `json:"abstract_source"`
	PublishedDate  *string         `json:"published_date"`
	FirstSeenAt    time.Time       `json:"first_seen_at"`
	ReadAt         *time.Time      `json:"read_at"`
	Raw            map[string]any  `json:"raw"`
	Classification Classification  `json:"classification"`
	SeenDate       string          `json:"seen_date"`
	FeedbackStatus *FeedbackStatus `json:"feedback_status"`
	ZoteroStatus   *ZoteroStatus   `json:"zotero_status"`
}

type Report struct {
	GeneratedAt   time.Time      `json:"generated_at"`
	LastUpdatedAt *time.Time     `json:"last_updated_at"`
	ReportDate    string         `json:"report_date"`
	Totals        map[string]int `json:"totals"`
	Papers        []ReportPaper  `json:"papers"`
	Errors        []string       `json:"errors"`
}

type FeedbackRecord struct {
	ID                 int64     `json:"id"`
	PaperID            int64     `json:"paper_id"`
	PaperTitle         string    `json:"paper_title"`
	OriginalRelevance  string    `json:"original_relevance"`
	CorrectedRelevance string    `json:"corrected_relevance"`
	Note               *string   `json:"note"`
	State              string    `json:"state"`
	UsedInProfile      bool      `json:"used_in_profile"`
	CreatedAt          time.Time `json:"created_at"`
}

type ProposalFeedbackContext struct {
	FeedbackID         int64
	PaperID            int64
	PaperTitle         string
	Journal            *string
	Abstract           *string
	OriginalRelevance  string
	CorrectedRelevance string
	Note               *string
}

type ProfileProposal struct {
	ID                 int64                    `json:"id"`
	Summary            string                   `json:"summary"`
	BaseProfileVersion int                      `json:"base_profile_version"`
	ProposedProfile    map[string]any           `json:"proposed_profile"`
	AppliedProfile     map[string]any           `json:"applied_profile,omitempty"`
	Changes            []profile.ProposalChange `json:"changes"`
	RuleDelta          map[string]any           `json:"rule_delta"`
	SourceFeedbackIDs  []int                    `json:"source_feedback_ids"`
	Model              string                   `json:"model"`
	State              string                   `json:"state"`
	CreatedAt          time.Time                `json:"created_at"`
	AppliedAt          *time.Time               `json:"applied_at"`
	RejectedAt         *time.Time               `json:"rejected_at"`
	AppliedVersion     *int                     `json:"applied_version"`
}

type paperRow struct {
	ID             int64
	SourceURL      string
	FeedTitle      *string
	Title          string
	URL            string
	DOI            *string
	Journal        *string
	AuthorsJSON    string
	Abstract       *string
	AbstractSource string
	PublishedDate  *string
	FirstSeenAt    time.Time
	ReadAt         *time.Time
	RawJSON        string
}

func Open(path string) (*Store, error) {
	// 打开现有 SQLite 数据库，并缓存表列信息用于旧 schema 兼容读取。
	return openExistingStore(path, storePoolRoleWrite)
}

func OpenRead(path string) (*Store, error) {
	// 为 API 读路径打开共享读 store，允许小型连接池承接并发读取。
	return openExistingStore(path, storePoolRoleRead)
}

func OpenWrite(path string) (*Store, error) {
	// 为 API 写路径打开单连接 store，避免并发写操作重新打出 SQLITE_BUSY。
	return openExistingStore(path, storePoolRoleWrite)
}

func openExistingStore(path string, role storePoolRole) (*Store, error) {
	clean := filepath.Clean(path)
	if _, err := os.Stat(clean); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteDSN(clean))
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	configureSQLiteDB(db, role)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}
	return buildStore(db)
}

func sqliteDSN(path string) string {
	values := url.Values{}
	values.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", sqliteBusyTimeoutMS))
	values.Add("_pragma", "journal_mode(WAL)")
	values.Add("_pragma", "synchronous(NORMAL)")
	return path + "?" + values.Encode()
}

func configureSQLiteDB(db *sql.DB, role storePoolRole) {
	// 读写共用 WAL/busy_timeout；写连接保持串行，读连接允许小型池并发读。
	maxOpenConns := writeStoreMaxOpenConn
	if role == storePoolRoleRead {
		maxOpenConns = readStoreMaxOpenConns
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxOpenConns)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)
}

func buildStore(db *sql.DB) (*Store, error) {
	store := &Store{db: db}
	var err error
	if store.paperColumns, err = loadColumns(db, "papers"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if store.classificationColumns, err = loadColumns(db, "classifications"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if store.feedbackColumns, err = loadColumns(db, "feedback"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if store.proposalColumns, err = loadColumns(db, "profile_proposals"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if store.zoteroColumns, err = loadColumns(db, "zotero_saves"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func scanPaperRow(scanner interface{ Scan(dest ...any) error }) (paperRow, error) {
	var row paperRow
	var feedTitle sql.NullString
	var doi sql.NullString
	var journal sql.NullString
	var abstract sql.NullString
	var publishedDate sql.NullString
	var firstSeenAt string
	var readAt sql.NullString
	if err := scanner.Scan(&row.ID, &row.SourceURL, &feedTitle, &row.Title, &row.URL, &doi, &journal, &row.AuthorsJSON, &abstract, &row.AbstractSource, &publishedDate, &firstSeenAt, &readAt, &row.RawJSON); err != nil {
		return paperRow{}, fmt.Errorf("scan paper row: %w", err)
	}
	parsedFirstSeenAt, err := parseTime(firstSeenAt)
	if err != nil {
		return paperRow{}, fmt.Errorf("parse paper first_seen_at: %w", err)
	}
	row.FeedTitle = nullableString(feedTitle)
	row.DOI = nullableString(doi)
	row.Journal = nullableString(journal)
	row.Abstract = nullableString(abstract)
	row.PublishedDate = nullableString(publishedDate)
	row.FirstSeenAt = parsedFirstSeenAt
	if row.AbstractSource == "" {
		row.AbstractSource = abstractSourceNone
	}
	if readAt.Valid {
		parsed, err := parseTime(readAt.String)
		if err != nil {
			return paperRow{}, fmt.Errorf("parse paper read_at: %w", err)
		}
		row.ReadAt = &parsed
	}
	return row, nil
}

func parseRawPayload(rawJSON string) (map[string]any, *string, []AbstractImage, error) {
	raw := map[string]any{}
	if strings.TrimSpace(rawJSON) != "" {
		if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
			return nil, nil, nil, err
		}
	}
	var abstractHTML *string
	if value, ok := raw["_abstract_html"].(string); ok && strings.TrimSpace(value) != "" {
		abstractHTML = &value
	}
	images := []AbstractImage{}
	if value, ok := raw["_abstract_images"]; ok {
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := json.Unmarshal(payload, &images); err != nil {
			return nil, nil, nil, err
		}
	}
	delete(raw, "_abstract_html")
	delete(raw, "_abstract_images")
	return raw, abstractHTML, images, nil
}

func decodeClassification(relevance string, confidence float64, reason string, topicTagsJSON string, recommendedAction string, model string, translatedTitleZH sql.NullString) (*Classification, error) {
	topicTags := []string{}
	if strings.TrimSpace(topicTagsJSON) != "" {
		if err := json.Unmarshal([]byte(topicTagsJSON), &topicTags); err != nil {
			return nil, err
		}
	}
	return &Classification{
		Relevance:         relevance,
		Confidence:        confidence,
		TopicTags:         topicTags,
		Reason:            reason,
		RecommendedAction: recommendedAction,
		Model:             model,
		TranslatedTitleZH: nullableString(translatedTitleZH),
	}, nil
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func loadColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, fmt.Errorf("query sqlite table info for %s: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, fmt.Errorf("scan sqlite table info for %s: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite table info for %s: %w", table, err)
	}
	return columns, nil
}

func (s *Store) columnExpr(columns map[string]bool, name string, fallback string) string {
	if columns[name] {
		return name
	}
	return fallback
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	clean := value.String
	return &clean
}

func stringPtr(value string) *string {
	clean := value
	return &clean
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func isSupportedRelevance(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case relevanceValue(0), relevanceValue(1), relevanceValue(2):
		return true
	default:
		return false
	}
}

func relevanceValue(index int) string {
	return [...]string{"direct", "indirect", "unrelated"}[index]
}
