package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/profile"
	_ "modernc.org/sqlite"
	"strings"
	"time"
)

func (s *Store) ListProfileProposals() ([]ProfileProposal, error) {
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

func (s *Store) GetProfileProposal(id int64) (*ProfileProposal, error) {
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

func (s *Store) SaveProfileProposal(summary string, baseProfileVersion int, proposedProfile map[string]any, changes []profile.ProposalChange, ruleDelta map[string]any, sourceFeedbackIDs []int64, model string, now time.Time) (*ProfileProposal, error) {
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

func (s *Store) ApplyProfileProposalState(proposalID int64, version int, appliedProfile map[string]any, changes []profile.ProposalChange, now time.Time) error {
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
