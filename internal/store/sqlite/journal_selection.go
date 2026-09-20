package sqlite

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/journals"
)

type PaperFilter struct {
	JournalKeys []string `json:"journal_keys"`
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
}

func (f PaperFilter) Bounds(location *time.Location) (*time.Time, *time.Time, error) {
	if len(f.JournalKeys) == 0 && f.DateFrom == "" && f.DateTo == "" {
		return nil, nil, fmt.Errorf("Select at least one journal or date boundary.")
	}
	parse := func(value string) (*time.Time, error) {
		if value == "" {
			return nil, nil
		}
		t, err := time.ParseInLocation("2006-01-02", value, location)
		if err != nil || t.Format("2006-01-02") != value {
			return nil, fmt.Errorf("Dates must use YYYY-MM-DD.")
		}
		return &t, nil
	}
	start, err := parse(f.DateFrom)
	if err != nil {
		return nil, nil, err
	}
	end, err := parse(f.DateTo)
	if err != nil {
		return nil, nil, err
	}
	if start != nil && end != nil && start.After(*end) {
		return nil, nil, fmt.Errorf("From must be on or before To.")
	}
	if end != nil {
		next := end.AddDate(0, 0, 1)
		end = &next
	}
	return start, end, nil
}

func (s *Store) JournalOptions() ([]journals.Identity, error) {
	rows, err := s.db.Query(`SELECT DISTINCT source_url, journal, feed_title FROM papers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	identities := map[string]journals.Identity{}
	for rows.Next() {
		var url string
		var name, feed sql.NullString
		if err := rows.Scan(&url, &name, &feed); err != nil {
			return nil, err
		}
		identity := journals.Resolve(url, nullableString(name), nullableString(feed))
		if old, ok := identities[identity.Key]; !ok || identity.Label < old.Label {
			identities[identity.Key] = identity
		}
	}
	result := make([]journals.Identity, 0, len(identities))
	for _, identity := range identities {
		result = append(result, identity)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Label) < strings.ToLower(result[j].Label) })
	return result, rows.Err()
}

type PaperSelection struct {
	PaperIDs     []int64 `json:"-"`
	Total        int     `json:"total"`
	Classified   int     `json:"classified"`
	Unclassified int     `json:"unclassified"`
	Fingerprint  string  `json:"fingerprint"`
	Timezone     string  `json:"timezone"`
}

// SelectCustomPapers reads only narrow metadata and uses the same resolver as
// reports. Existing subscriptions are deliberately irrelevant to selection.
func (s *Store) SelectCustomPapers(f PaperFilter, location *time.Location) (PaperSelection, error) {
	result := PaperSelection{PaperIDs: []int64{}, Timezone: TimezoneLabel(time.Now(), location)}
	start, end, err := f.Bounds(location)
	if err != nil {
		return result, err
	}
	selected := map[string]bool{}
	for _, key := range f.JournalKeys {
		selected[key] = true
	}
	options, err := s.JournalOptions()
	if err != nil {
		return result, err
	}
	known := map[string]bool{}
	for _, option := range options {
		known[option.Key] = true
	}
	for key := range selected {
		if !known[key] {
			return result, fmt.Errorf("Unknown journal selection. Refresh journals and try again.")
		}
	}
	query := `SELECT p.id, p.source_url, p.journal, p.feed_title,
 EXISTS(SELECT 1 FROM classifications c WHERE c.paper_id=p.id) FROM papers p WHERE 1=1`
	args := []any{}
	// julianday handles legacy timestamps with different offsets/fraction widths.
	if start != nil {
		query += ` AND julianday(p.first_seen_at)>=julianday(?)`
		args = append(args, start.UTC().Format(time.RFC3339Nano))
	}
	if end != nil {
		query += ` AND julianday(p.first_seen_at)<julianday(?)`
		args = append(args, end.UTC().Format(time.RFC3339Nano))
	}
	query += ` ORDER BY p.first_seen_at DESC, p.id DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var url string
		var name, feed sql.NullString
		var classified bool
		if err := rows.Scan(&id, &url, &name, &feed, &classified); err != nil {
			return result, err
		}
		identity := journals.Resolve(url, nullableString(name), nullableString(feed))
		if len(selected) > 0 && !selected[identity.Key] {
			continue
		}
		result.PaperIDs = append(result.PaperIDs, id)
		if classified {
			result.Classified++
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.Total = len(result.PaperIDs)
	result.Unclassified = result.Total - result.Classified
	ids := append([]int64{}, result.PaperIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result.Fingerprint = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprint(ids))))
	return result, nil
}

// TimezoneLabel keeps the local offset clear even when the OS only names the
// zone "Local".
func TimezoneLabel(now time.Time, location *time.Location) string {
	return "UTC" + now.In(location).Format("-07:00")
}
