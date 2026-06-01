package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yyngfive/scirssagent/internal/profile"
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
	// 关闭底层数据库连接。
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

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

func (s *Store) ListFeedback() ([]FeedbackRecord, error) {
	// 读取 feedback 列表，顺序与 Python API 保持一致。
	if len(s.feedbackColumns) == 0 || len(s.paperColumns) == 0 {
		return []FeedbackRecord{}, nil
	}
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT f.id, f.paper_id, p.title AS paper_title,
			f.original_relevance, f.corrected_relevance, f.note,
			%s AS state, %s AS used_in_prompt, f.created_at
		FROM feedback f
		JOIN papers p ON p.id = f.paper_id
		ORDER BY f.created_at DESC, f.id DESC
	`, s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen)), s.columnExpr(s.feedbackColumns, "used_in_prompt", "0")))
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

func (s *Store) ListProfileProposals() ([]ProfileProposal, error) {
	// 读取全部 profile proposals，按创建时间倒序排列。
	if len(s.proposalColumns) == 0 {
		return []ProfileProposal{}, nil
	}
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT id, summary, proposed_profile_json,
			%s AS rule_delta_json, %s AS base_profile_version, %s AS change_set_json,
			%s AS applied_profile_json, source_feedback_ids_json, model,
			%s AS state, created_at, %s AS applied_at, %s AS rejected_at, %s AS applied_version
		FROM profile_proposals
		ORDER BY created_at DESC, id DESC
	`, s.columnExpr(s.proposalColumns, "rule_delta_json", "NULL"),
		s.columnExpr(s.proposalColumns, "base_profile_version", "0"),
		s.columnExpr(s.proposalColumns, "change_set_json", "NULL"),
		s.columnExpr(s.proposalColumns, "applied_profile_json", "NULL"),
		s.columnExpr(s.proposalColumns, "state", quote(proposalStatePending)),
		s.columnExpr(s.proposalColumns, "applied_at", "NULL"),
		s.columnExpr(s.proposalColumns, "rejected_at", "NULL"),
		s.columnExpr(s.proposalColumns, "applied_version", "NULL")))
	if err != nil {
		return nil, fmt.Errorf("query profile proposals: %w", err)
	}
	defer rows.Close()

	items := []ProfileProposal{}
	for rows.Next() {
		item, found, err := scanProfileProposalRow(rows)
		if err != nil {
			return nil, err
		}
		if found && item != nil {
			items = append(items, *item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate profile proposals: %w", err)
	}
	return items, nil
}

func (s *Store) ListOpenFeedbackContexts() ([]ProposalFeedbackContext, error) {
	// 读取 proposal generate 需要的 open feedback 上下文。
	if len(s.feedbackColumns) == 0 || len(s.paperColumns) == 0 {
		return []ProposalFeedbackContext{}, nil
	}
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT f.id, f.paper_id, p.title AS paper_title, p.journal, p.abstract,
			f.original_relevance, f.corrected_relevance, f.note
		FROM feedback f
		JOIN papers p ON p.id = f.paper_id
		WHERE %s = ?
		ORDER BY f.created_at DESC, f.id DESC
	`, s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen))), feedbackStateOpen)
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
		if err := rows.Scan(
			&item.FeedbackID,
			&item.PaperID,
			&item.PaperTitle,
			&journal,
			&abstract,
			&item.OriginalRelevance,
			&item.CorrectedRelevance,
			&note,
		); err != nil {
			return nil, fmt.Errorf("scan open feedback context: %w", err)
		}
		item.Journal = nullableString(journal)
		item.Abstract = nullableString(abstract)
		item.Note = nullableString(note)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open feedback contexts: %w", err)
	}
	return items, nil
}

func (s *Store) GetProfileProposal(id int64) (*ProfileProposal, error) {
	// 读取单个 profile proposal，包含 proposal profile 和 delta JSON。
	if len(s.proposalColumns) == 0 {
		return nil, nil
	}
	row := s.db.QueryRow(fmt.Sprintf(`
		SELECT id, summary, proposed_profile_json,
			%s AS rule_delta_json, %s AS base_profile_version, %s AS change_set_json,
			%s AS applied_profile_json, source_feedback_ids_json, model,
			%s AS state, created_at, %s AS applied_at, %s AS rejected_at, %s AS applied_version
		FROM profile_proposals
		WHERE id = ?
	`, s.columnExpr(s.proposalColumns, "rule_delta_json", "NULL"),
		s.columnExpr(s.proposalColumns, "base_profile_version", "0"),
		s.columnExpr(s.proposalColumns, "change_set_json", "NULL"),
		s.columnExpr(s.proposalColumns, "applied_profile_json", "NULL"),
		s.columnExpr(s.proposalColumns, "state", quote(proposalStatePending)),
		s.columnExpr(s.proposalColumns, "applied_at", "NULL"),
		s.columnExpr(s.proposalColumns, "rejected_at", "NULL"),
		s.columnExpr(s.proposalColumns, "applied_version", "NULL")), id)

	proposal, found, err := scanProfileProposalRow(row)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return proposal, nil
}

func (s *Store) FeedbackByID(id int64) (*FeedbackRecord, error) {
	// 按 id 读取单条 feedback，供写接口返回最新行。
	if len(s.feedbackColumns) == 0 || len(s.paperColumns) == 0 {
		return nil, nil
	}
	row := s.db.QueryRow(fmt.Sprintf(`
		SELECT f.id, f.paper_id, p.title AS paper_title,
			f.original_relevance, f.corrected_relevance, f.note,
			%s AS state, %s AS used_in_prompt, f.created_at
		FROM feedback f
		JOIN papers p ON p.id = f.paper_id
		WHERE f.id = ?
	`, s.columnExpr(s.feedbackColumns, "state", quote(feedbackStateOpen)), s.columnExpr(s.feedbackColumns, "used_in_prompt", "0")), id)
	record, err := scanFeedbackRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "sql: no rows in result set") {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (s *Store) LatestClassification(paperID int64) (*Classification, error) {
	// 暴露单篇 paper 的最新 classification，供 Zotero/API 写路径复用。
	return s.latestClassification(paperID)
}

func (s *Store) LatestZoteroStatus(paperID int64) (*ZoteroStatus, error) {
	// 暴露单篇 paper 的最新 Zotero 保存状态，供 API/CLI 避免重复保存。
	return s.latestZoteroStatus(paperID)
}

func (s *Store) UpsertZoteroStatus(paperID int64, state string, itemKey *string, errorMessage *string, now time.Time) (*ZoteroStatus, error) {
	// 以单 paper 单记录的方式刷新 Zotero 保存状态，语义与 Python upsert 对齐。
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

func (s *Store) CreateFeedback(paperID int64, correctedRelevance string, note *string, now time.Time) (*FeedbackRecord, error) {
	// 创建一条新的 feedback，并从最新 classification 回填 original_relevance。
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
	result, err := s.db.Exec(`
		INSERT INTO feedback (
			paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, paperID, classification.Relevance, correctedRelevance, note, feedbackStateOpen, 0, now.UTC().Format(time.RFC3339Nano))
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

func (s *Store) SaveProfileProposal(summary string, baseProfileVersion int, proposedProfile map[string]any, changes []profile.ProposalChange, ruleDelta map[string]any, sourceFeedbackIDs []int64, model string, now time.Time) (*ProfileProposal, error) {
	// 保存一条新的 pending profile proposal，并回读标准化后的记录。
	proposedDocument, err := profile.ValidateMap(proposedProfile)
	if err != nil {
		return nil, fmt.Errorf("validate proposed profile: %w", err)
	}
	normalizedChanges, err := profile.ValidateProposalChanges(changes)
	if err != nil {
		return nil, fmt.Errorf("validate profile proposal changes: %w", err)
	}
	ruleDeltaPayload, err := profile.ValidateProposalDeltaMap(ruleDelta, summary)
	if err != nil {
		return nil, fmt.Errorf("validate profile proposal delta: %w", err)
	}
	profileJSON, err := json.Marshal(proposedDocument)
	if err != nil {
		return nil, fmt.Errorf("encode proposed profile: %w", err)
	}
	ruleDeltaJSON, err := json.Marshal(ruleDeltaPayload)
	if err != nil {
		return nil, fmt.Errorf("encode profile proposal delta: %w", err)
	}
	changeSetJSON, err := json.Marshal(normalizedChanges)
	if err != nil {
		return nil, fmt.Errorf("encode profile proposal changes: %w", err)
	}
	sourceIDs := make([]int64, 0, len(sourceFeedbackIDs))
	seen := map[int64]struct{}{}
	for _, id := range sourceFeedbackIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		sourceIDs = append(sourceIDs, id)
	}
	sourceIDsJSON, err := json.Marshal(sourceIDs)
	if err != nil {
		return nil, fmt.Errorf("encode source feedback ids: %w", err)
	}
	result, err := s.db.Exec(`
		INSERT INTO profile_proposals (
			summary, proposed_profile_json, rule_delta_json, base_profile_version,
			change_set_json, source_feedback_ids_json, model, state, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, strings.TrimSpace(summary), string(profileJSON), string(ruleDeltaJSON), baseProfileVersion, string(changeSetJSON), string(sourceIDsJSON), strings.TrimSpace(model), proposalStatePending, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("insert profile proposal: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read profile proposal id: %w", err)
	}
	item, err := s.GetProfileProposal(id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("profile proposal disappeared after insert")
	}
	return item, nil
}

func (s *Store) DeleteFeedback(id int64) error {
	// 删除单条 feedback，不存在时返回显式错误。
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

func (s *Store) MarkPaperRead(paperID int64, now time.Time) (time.Time, error) {
	// 以幂等方式设置 read_at，重复调用保留首次已写入的时间。
	result, err := s.db.Exec(`
		UPDATE papers
		SET read_at = COALESCE(read_at, ?)
		WHERE id = ?
	`, now.UTC().Format(time.RFC3339Nano), paperID)
	if err != nil {
		return time.Time{}, fmt.Errorf("update paper read status: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return time.Time{}, fmt.Errorf("update paper read status: %w", err)
	}
	if rowsAffected == 0 {
		return time.Time{}, ErrPaperNotFound
	}
	var readAt string
	if err := s.db.QueryRow(`SELECT read_at FROM papers WHERE id = ?`, paperID).Scan(&readAt); err != nil {
		return time.Time{}, fmt.Errorf("reload paper read status: %w", err)
	}
	return parseTime(readAt)
}

func (s *Store) ApplyProfileProposalState(proposalID int64, version int, appliedProfile map[string]any, changes []profile.ProposalChange, now time.Time) error {
	// 把 proposal 标成 applied，并记录 applied_at/applied_version。
	var appliedProfileJSON any
	if appliedProfile != nil {
		normalizedProfile, err := profile.ValidateMap(appliedProfile)
		if err != nil {
			return fmt.Errorf("validate applied profile: %w", err)
		}
		data, err := json.Marshal(normalizedProfile)
		if err != nil {
			return fmt.Errorf("encode applied profile: %w", err)
		}
		appliedProfileJSON = string(data)
	}
	normalizedChanges, err := profile.ValidateProposalChanges(changes)
	if err != nil {
		return fmt.Errorf("validate applied proposal changes: %w", err)
	}
	changeSetJSON, err := json.Marshal(normalizedChanges)
	if err != nil {
		return fmt.Errorf("encode applied proposal changes: %w", err)
	}
	query := `
		UPDATE profile_proposals
		SET state = ?, applied_at = ?, applied_version = ?, rejected_at = NULL
	`
	args := []any{proposalStateApplied, now.UTC().Format(time.RFC3339Nano), version}
	if s.proposalColumns["change_set_json"] {
		query += `, change_set_json = ?`
		args = append(args, string(changeSetJSON))
	}
	if s.proposalColumns["applied_profile_json"] {
		query += `, applied_profile_json = ?`
		args = append(args, appliedProfileJSON)
	}
	query += ` WHERE id = ?`
	args = append(args, proposalID)
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("apply profile proposal state: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("apply profile proposal state: %w", err)
	}
	if rowsAffected == 0 {
		return ErrProfileProposalNotFound
	}
	return nil
}

func (s *Store) RejectProfileProposalState(proposalID int64, now time.Time) error {
	// 把 proposal 标成 rejected，并写入 rejected_at。
	result, err := s.db.Exec(`
		UPDATE profile_proposals
		SET state = ?, rejected_at = ?
		WHERE id = ?
	`, proposalStateRejected, now.UTC().Format(time.RFC3339Nano), proposalID)
	if err != nil {
		return fmt.Errorf("reject profile proposal state: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reject profile proposal state: %w", err)
	}
	if rowsAffected == 0 {
		return ErrProfileProposalNotFound
	}
	return nil
}

func (s *Store) MarkFeedbackUsed(ids []int64) error {
	// 把一组 feedback 标成 used，供 proposal apply 后复用。
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
	// 根据 feedback id 列表取去重后的 paper ids，顺序和 Python 保持一致。
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

func scanFeedbackRecord(scanner interface{ Scan(dest ...any) error }) (FeedbackRecord, error) {
	var record FeedbackRecord
	var note sql.NullString
	var usedInPrompt int64
	var createdAt string
	if err := scanner.Scan(&record.ID, &record.PaperID, &record.PaperTitle, &record.OriginalRelevance, &record.CorrectedRelevance, &note, &record.State, &usedInPrompt, &createdAt); err != nil {
		return FeedbackRecord{}, fmt.Errorf("scan feedback row: %w", err)
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return FeedbackRecord{}, fmt.Errorf("parse feedback created_at: %w", err)
	}
	record.Note = nullableString(note)
	record.UsedInProfile = usedInPrompt != 0
	record.CreatedAt = parsed
	return record, nil
}

func scanProfileProposalRow(scanner interface{ Scan(dest ...any) error }) (*ProfileProposal, bool, error) {
	var proposal ProfileProposal
	var proposedProfileJSON string
	var ruleDeltaJSON sql.NullString
	var baseProfileVersion sql.NullInt64
	var changeSetJSON sql.NullString
	var appliedProfileJSON sql.NullString
	var sourceFeedbackIDsJSON string
	var createdAt string
	var appliedAt sql.NullString
	var rejectedAt sql.NullString
	var appliedVersion sql.NullInt64
	err := scanner.Scan(&proposal.ID, &proposal.Summary, &proposedProfileJSON, &ruleDeltaJSON, &baseProfileVersion, &changeSetJSON, &appliedProfileJSON, &sourceFeedbackIDsJSON, &proposal.Model, &proposal.State, &createdAt, &appliedAt, &rejectedAt, &appliedVersion)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("scan profile proposal: %w", err)
	}
	parsedCreatedAt, err := parseTime(createdAt)
	if err != nil {
		return nil, false, fmt.Errorf("parse created_at for proposal %d: %w", proposal.ID, err)
	}
	proposedProfile, err := profile.ValidateBytes([]byte(proposedProfileJSON))
	if err != nil {
		return nil, false, fmt.Errorf("parse proposed profile for proposal %d: %w", proposal.ID, err)
	}
	ruleDelta, err := profile.ValidateProposalDeltaBytes([]byte(ruleDeltaJSON.String), proposal.Summary)
	if err != nil {
		return nil, false, fmt.Errorf("parse rule delta for proposal %d: %w", proposal.ID, err)
	}
	changes, err := profile.ValidateProposalChangesBytes([]byte(changeSetJSON.String))
	if err != nil {
		return nil, false, fmt.Errorf("parse change set for proposal %d: %w", proposal.ID, err)
	}
	sourceFeedbackIDs, err := decodeIntSlice(sourceFeedbackIDsJSON)
	if err != nil {
		return nil, false, fmt.Errorf("parse source feedback ids for proposal %d: %w", proposal.ID, err)
	}
	proposal.ProposedProfile = proposedProfile
	if baseProfileVersion.Valid {
		proposal.BaseProfileVersion = int(baseProfileVersion.Int64)
	}
	if appliedProfileJSON.Valid && strings.TrimSpace(appliedProfileJSON.String) != "" {
		appliedProfile, err := profile.ValidateBytes([]byte(appliedProfileJSON.String))
		if err != nil {
			return nil, false, fmt.Errorf("parse applied profile for proposal %d: %w", proposal.ID, err)
		}
		proposal.AppliedProfile = appliedProfile
	}
	proposal.Changes = changes
	proposal.RuleDelta = ruleDelta
	proposal.SourceFeedbackIDs = sourceFeedbackIDs
	proposal.CreatedAt = parsedCreatedAt
	if appliedAt.Valid {
		parsed, err := parseTime(appliedAt.String)
		if err != nil {
			return nil, false, fmt.Errorf("parse applied_at for proposal %d: %w", proposal.ID, err)
		}
		proposal.AppliedAt = &parsed
	}
	if rejectedAt.Valid {
		parsed, err := parseTime(rejectedAt.String)
		if err != nil {
			return nil, false, fmt.Errorf("parse rejected_at for proposal %d: %w", proposal.ID, err)
		}
		proposal.RejectedAt = &parsed
	}
	if appliedVersion.Valid {
		value := int(appliedVersion.Int64)
		proposal.AppliedVersion = &value
	}
	return &proposal, true, nil
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

func decodeFeedbackStatus(correctedRelevance *string, note sql.NullString, createdAt *string, state *string, usedInPrompt sql.NullInt64) (*FeedbackStatus, error) {
	if correctedRelevance == nil && !note.Valid && createdAt == nil && state == nil && !usedInPrompt.Valid {
		return nil, nil
	}
	status := &FeedbackStatus{
		HasFeedback:        true,
		CorrectedRelevance: correctedRelevance,
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

func decodeIntSlice(raw string) ([]int, error) {
	values := []int{}
	if strings.TrimSpace(raw) == "" {
		return values, nil
	}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
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

func FeedbackIDsToInt64(ids []int) []int64 {
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		result = append(result, int64(id))
	}
	return result
}
