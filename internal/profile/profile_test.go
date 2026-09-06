package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadCurrentReturnsNilWhenMissing(t *testing.T) {
	payload, err := ReadCurrent(filepath.Join(t.TempDir(), "classification_profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		t.Fatalf("expected nil payload, got %#v", payload)
	}
}

func TestReadCurrentValidatesProfile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "classification_profile.json")
	if err := os.WriteFile(path, []byte(`{
  "meta": {
    "name": "Test",
    "version": 1,
    "created_at": "2026-05-16T00:00:00Z",
    "updated_at": "2026-05-16T00:00:00Z",
    "source_description": "Fixture"
  },
  "scope": "RNA biology",
  "relevance_rules": {"direct": ["RNA"], "indirect": [], "unrelated": []},
  "topic_taxonomy": [{"id": "rna_bio", "label": "RNA Bio"}],
  "few_shots": []
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	payload, err := ReadCurrent(path)
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := payload["meta"].(map[string]any)
	if !ok || meta["name"] != "Test" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestReadCurrentRejectsInvalidJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "classification_profile.json")
	if err := os.WriteFile(path, []byte(`{"meta":`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadCurrent(path); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestValidateProposalDeltaBytesFallsBackForLegacyRows(t *testing.T) {
	payload, err := ValidateProposalDeltaBytes(nil, "Legacy summary")
	if err != nil {
		t.Fatal(err)
	}
	if payload["summary"] != "Legacy summary" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestWriteCurrentCompactsPersistedShape(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "classification_profile.json")

	err := WriteCurrent(path, map[string]any{
		"meta": map[string]any{
			"name":               " Test Profile ",
			"version":            2,
			"created_at":         "2026-05-16T00:00:00Z",
			"updated_at":         "2026-05-16T01:00:00Z",
			"source_description": " Example source ",
		},
		"scope": " RNA biology ",
		"relevance_rules": map[string]any{
			"direct":    []any{"RNA chemistry", "RNA chemistry"},
			"indirect":  []any{" General biology "},
			"unrelated": []any{},
		},
		"topic_taxonomy": []any{
			map[string]any{"id": "rna-bio", "label": " RNA Bio "},
			map[string]any{"id": "rna bio", "label": "Duplicate"},
		},
		"few_shots": []any{
			map[string]any{"title": " One ", "relevance": "direct", "tags": []any{"rna-bio", "rna bio"}, "rationale": " A "},
			map[string]any{"title": " Two ", "relevance": "indirect", "tags": []any{"bio"}, "rationale": " B "},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	payload, err := ReadCurrent(path)
	if err != nil {
		t.Fatal(err)
	}
	meta := payload["meta"].(map[string]any)
	if meta["name"] != "Test Profile" || meta["version"] != float64(2) {
		t.Fatalf("unexpected meta: %#v", meta)
	}
	taxonomy := payload["topic_taxonomy"].([]any)
	// topic_taxonomy 是活的注册表：写回保留并做 trim/去重，不再清空。
	if len(taxonomy) != 2 {
		t.Fatalf("unexpected taxonomy: %#v", taxonomy)
	}
	first := taxonomy[0].(map[string]any)
	second := taxonomy[1].(map[string]any)
	if first["id"] != "rna-bio" || first["label"] != "RNA Bio" {
		t.Fatalf("unexpected first topic: %#v", first)
	}
	if second["id"] != "rna bio" || second["label"] != "Duplicate" {
		t.Fatalf("unexpected second topic: %#v", second)
	}
	directRules := payload["relevance_rules"].(map[string]any)["direct"].([]any)
	if len(directRules) != 1 {
		t.Fatalf("unexpected direct rules: %#v", directRules)
	}
	directRule := directRules[0].(map[string]any)
	if directRule["text"] != "RNA chemistry" {
		t.Fatalf("unexpected direct rule: %#v", directRule)
	}
	fewShots := payload["few_shots"].([]any)
	if len(fewShots) != 0 {
		t.Fatalf("unexpected few_shots: %#v", fewShots)
	}
}

func TestPrepareAppliedProfileIncrementsVersionAndPreservesCreatedAt(t *testing.T) {
	now := time.Date(2026, 5, 17, 8, 30, 0, 0, time.UTC)
	proposed := map[string]any{
		"meta": map[string]any{
			"name":               "Proposal",
			"version":            1,
			"created_at":         "2026-05-10T00:00:00Z",
			"updated_at":         "2026-05-10T00:00:00Z",
			"source_description": "proposal",
		},
		"scope": "RNA biology",
		"relevance_rules": map[string]any{
			"direct":    []any{"RNA"},
			"indirect":  []any{},
			"unrelated": []any{},
		},
		"topic_taxonomy": []any{},
		"few_shots":      []any{},
	}
	current := map[string]any{
		"meta": map[string]any{
			"name":               "Current",
			"version":            4,
			"created_at":         "2026-05-01T00:00:00Z",
			"updated_at":         "2026-05-12T00:00:00Z",
			"source_description": "current",
		},
		"scope": "RNA biology",
		"relevance_rules": map[string]any{
			"direct":    []any{"RNA"},
			"indirect":  []any{},
			"unrelated": []any{},
		},
		"topic_taxonomy": []any{},
		"few_shots":      []any{},
	}

	applied, version, err := PrepareAppliedProfile(proposed, current, now)
	if err != nil {
		t.Fatal(err)
	}
	if version != 5 {
		t.Fatalf("version = %d", version)
	}
	meta := applied["meta"].(map[string]any)
	if meta["version"] != float64(5) || meta["created_at"] != "2026-05-01T00:00:00Z" || meta["updated_at"] != now.Format(time.RFC3339) {
		t.Fatalf("unexpected applied meta: %#v", meta)
	}
}

func TestPrepareUpdatedProfilePreservesCreatedAtAndSourceDescription(t *testing.T) {
	now := time.Date(2026, 5, 21, 11, 45, 0, 0, time.UTC)
	edited := map[string]any{
		"meta": map[string]any{
			"name":               "Edited Profile",
			"version":            1,
			"created_at":         "2026-05-20T00:00:00Z",
			"updated_at":         "2026-05-20T00:00:00Z",
			"source_description": "edited",
		},
		"scope": "RNA biology and splicing",
		"relevance_rules": map[string]any{
			"direct":    []any{"RNA", "Splicing"},
			"indirect":  []any{"Protein complexes"},
			"unrelated": []any{"Plant biology"},
		},
		"topic_taxonomy": []any{map[string]any{"id": "rna_bio", "label": "RNA Bio"}},
		"few_shots": []any{
			map[string]any{
				"title":     "Example paper",
				"relevance": "direct",
				"tags":      []any{"rna_bio"},
				"rationale": "Tracks RNA mechanisms.",
			},
		},
	}
	current := map[string]any{
		"meta": map[string]any{
			"name":               "Current",
			"version":            2,
			"created_at":         "2026-05-01T00:00:00Z",
			"updated_at":         "2026-05-12T00:00:00Z",
			"source_description": "current profile",
		},
		"scope": "RNA biology",
		"relevance_rules": map[string]any{
			"direct":    []any{"RNA"},
			"indirect":  []any{},
			"unrelated": []any{},
		},
		"topic_taxonomy": []any{},
		"few_shots":      []any{},
	}

	updated, version, err := PrepareUpdatedProfile(edited, current, now)
	if err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("version = %d", version)
	}
	meta := updated["meta"].(map[string]any)
	if meta["name"] != "Edited Profile" {
		t.Fatalf("name = %#v", meta["name"])
	}
	if meta["version"] != float64(3) {
		t.Fatalf("version meta = %#v", meta["version"])
	}
	if meta["created_at"] != "2026-05-01T00:00:00Z" {
		t.Fatalf("created_at = %#v", meta["created_at"])
	}
	if meta["updated_at"] != now.Format(time.RFC3339) {
		t.Fatalf("updated_at = %#v", meta["updated_at"])
	}
	if meta["source_description"] != "current profile" {
		t.Fatalf("source_description = %#v", meta["source_description"])
	}
	// topic_taxonomy 是活的注册表：手动编辑保留它；few_shots 仍然清空。
	taxonomy := updated["topic_taxonomy"].([]any)
	if len(taxonomy) != 1 {
		t.Fatalf("taxonomy should be preserved: %#v", updated)
	}
	topic := taxonomy[0].(map[string]any)
	if topic["id"] != "rna_bio" || topic["label"] != "RNA Bio" {
		t.Fatalf("unexpected topic: %#v", topic)
	}
	if len(updated["few_shots"].([]any)) != 0 {
		t.Fatalf("few_shots should be cleared: %#v", updated)
	}
}

func TestValidateProposalChangesNormalizesRestoreOperation(t *testing.T) {
	changes, err := ValidateProposalChanges([]ProposalChange{
		{
			ID:                "restore-negative-boundary",
			Section:           ProposalSectionUnrelatedRule,
			Operation:         "restore",
			Summary:           "Restore a negative boundary.",
			TextAfter:         []string{"Protein probe papers are unrelated without nucleic acid chemistry."},
			Rationale:         "Validator required the negative boundary to be restored.",
			SourceFeedbackIDs: []int64{1},
			SourcePaperIDs:    []int64{10},
			Status:            ProposalStatusProposed,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Operation != ProposalOperationAdd {
		t.Fatalf("expected restore to normalize to add, got %#v", changes)
	}
}

func TestValidateProposalChangesAcceptsScalarTextFields(t *testing.T) {
	changes, err := ValidateProposalChangesBytes([]byte(`[
    {
      "id":"rewrite-negative-boundary",
      "section":"unrelated_rule",
      "operation":"rewrite",
      "summary":"Clarify negative boundary.",
      "text_before":"Old broad boundary.",
      "text_after":"Surface adjacency alone is unrelated without a core nucleic-acid-method contribution.",
      "topic_before":[],
      "topic_after":[],
      "rationale":"The model may emit a scalar string for single-rule rewrites.",
      "source_feedback_ids":[1],
      "source_paper_ids":[10],
      "status":"proposed"
    }
  ]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || len(changes[0].TextBefore) != 1 || changes[0].TextBefore[0] != "Old broad boundary." {
		t.Fatalf("expected scalar text_before to normalize to list, got %#v", changes)
	}
	if len(changes[0].TextAfter) != 1 || !strings.Contains(changes[0].TextAfter[0], "Surface adjacency") {
		t.Fatalf("expected scalar text_after to normalize to list, got %#v", changes)
	}
}

func TestParseLegacyStringRulesMigratesToStructuredRules(t *testing.T) {
	// 旧 profile：规则是纯字符串、taxonomy 为空。载入应透明迁移为结构化规则。
	document, err := parseDocumentBytes([]byte(`{
		"meta":{"name":"Legacy","version":24,"created_at":"2026-05-01T00:00:00Z","updated_at":"2026-05-01T00:00:00Z","source_description":"legacy"},
		"scope":"RNA biology",
		"relevance_rules":{"direct":["RNA chemistry"],"indirect":[],"unrelated":["Plant biology"]},
		"topic_taxonomy":[],
		"few_shots":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(document.RelevanceRules.Direct) != 1 || document.RelevanceRules.Direct[0].Text != "RNA chemistry" {
		t.Fatalf("unexpected direct rules: %#v", document.RelevanceRules.Direct)
	}
	if document.RelevanceRules.Direct[0].TopicIDs == nil || len(document.RelevanceRules.Direct[0].TopicIDs) != 0 {
		t.Fatalf("expected empty topic ids, got %#v", document.RelevanceRules.Direct[0].TopicIDs)
	}
}

func TestProfileValidateRejectsUnknownTopicRefAndReservedID(t *testing.T) {
	// 规则引用未注册主题 → 拒绝。
	_, err := parseDocumentBytes([]byte(`{
		"meta":{"name":"Bad","version":1,"created_at":"2026-05-01T00:00:00Z","updated_at":"2026-05-01T00:00:00Z","source_description":"x"},
		"scope":"RNA biology",
		"relevance_rules":{"direct":[{"text":"RNA","topics":["t-ghost01"]}],"indirect":[],"unrelated":[]},
		"topic_taxonomy":[{"id":"t-real0001","label":"Real"}],
		"few_shots":[]
	}`))
	if err == nil || !strings.Contains(err.Error(), "unknown topic id") {
		t.Fatalf("expected unknown topic id error, got %v", err)
	}
	// 保留哨兵 id 不可作为注册表条目。
	_, err = parseDocumentBytes([]byte(`{
		"meta":{"name":"Bad","version":1,"created_at":"2026-05-01T00:00:00Z","updated_at":"2026-05-01T00:00:00Z","source_description":"x"},
		"scope":"RNA biology",
		"relevance_rules":{"direct":[],"indirect":[],"unrelated":[]},
		"topic_taxonomy":[{"id":"none","label":"None"}],
		"few_shots":[]
	}`))
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected reserved id error, got %v", err)
	}
}

func TestParseAssignsIDsToLabelOnlyTopics(t *testing.T) {
	document, err := parseDocumentBytes([]byte(`{
		"meta":{"name":"Bootstrap","version":1,"created_at":"2026-05-01T00:00:00Z","updated_at":"2026-05-01T00:00:00Z","source_description":"x"},
		"scope":"RNA biology",
		"relevance_rules":{"direct":[{"text":"RNA chemistry","topics":["RNA Bio"]}],"indirect":[],"unrelated":[]},
		"topic_taxonomy":[{"label":"RNA Bio"}],
		"few_shots":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(document.TopicTaxonomy) != 1 || document.TopicTaxonomy[0].ID == "" {
		t.Fatalf("expected label-only topic to receive an id: %#v", document.TopicTaxonomy)
	}
}

func TestAppendTopicEntryIsIdempotentByLabel(t *testing.T) {
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	current := map[string]any{
		"meta": map[string]any{
			"name": "Current", "version": 2, "created_at": "2026-05-01T00:00:00Z",
			"updated_at": "2026-05-12T00:00:00Z", "source_description": "current",
		},
		"scope":           "RNA biology",
		"relevance_rules": map[string]any{"direct": []any{}, "indirect": []any{}, "unrelated": []any{}},
		"topic_taxonomy":  []any{map[string]any{"id": "t-aaa00001", "label": "RNA Bio"}},
		"few_shots":       []any{},
	}
	updated, topic, changed, err := AppendTopicEntry(current, " Splicing ", now)
	if err != nil || !changed {
		t.Fatalf("expected new topic, changed=true, err=%v", err)
	}
	if topic.ID == "" || topic.Label != "Splicing" {
		t.Fatalf("unexpected topic: %#v", topic)
	}
	if version := updated["meta"].(map[string]any)["version"]; version != float64(3) {
		t.Fatalf("expected version bump, got %#v", version)
	}
	_, existing, changed, err := AppendTopicEntry(current, "rna bio", now)
	if err != nil || changed {
		t.Fatalf("expected existing label to be idempotent, changed=%v err=%v", changed, err)
	}
	if existing.ID != "t-aaa00001" || existing.Label != "RNA Bio" {
		t.Fatalf("unexpected existing topic: %#v", existing)
	}
}

func TestPrepareAppliedProfileFromChangesResolvesTopicLabels(t *testing.T) {
	current := map[string]any{
		"meta": map[string]any{
			"name": "Current", "version": 4, "created_at": "2026-05-01T00:00:00Z",
			"updated_at": "2026-05-12T00:00:00Z", "source_description": "current",
		},
		"scope": "RNA biology",
		"relevance_rules": map[string]any{
			"direct":    []any{map[string]any{"text": "RNA chemistry", "topics": []any{"t-aaa00001"}}},
			"indirect":  []any{},
			"unrelated": []any{"Plant biology"},
		},
		"topic_taxonomy": []any{map[string]any{"id": "t-aaa00001", "label": "RNA Bio"}},
		"few_shots":      []any{},
	}
	changes, err := ValidateProposalChanges([]ProposalChange{
		{
			ID: "add-topic", Section: ProposalSectionTopic, Operation: ProposalOperationAdd,
			Summary: "Add topic.", Rationale: "No existing topic covers splicing.",
			TopicAfter: []topicDefinition{{Label: "Splicing"}},
		},
		{
			ID: "tag-rule", Section: ProposalSectionIndirectRule, Operation: ProposalOperationAdd,
			Summary: "Add splicing rule.", Rationale: "Topic feedback.",
			TextAfter:   []string{"Splicing mechanism papers that inform RNA engineering."},
			TopicsAfter: []string{"splicing"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	changes, err = FinalizeProposalChanges(changes, []string{"add-topic", "tag-rule"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	applied, version, err := PrepareAppliedProfileFromChanges(current, changes, time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if version != 5 {
		t.Fatalf("version = %d", version)
	}
	taxonomy := applied["topic_taxonomy"].([]any)
	if len(taxonomy) != 2 {
		t.Fatalf("expected new topic appended: %#v", taxonomy)
	}
	splicing := taxonomy[1].(map[string]any)
	if splicing["label"] != "Splicing" || splicing["id"] == "" {
		t.Fatalf("unexpected splicing topic: %#v", splicing)
	}
	indirect := applied["relevance_rules"].(map[string]any)["indirect"].([]any)
	if len(indirect) != 1 {
		t.Fatalf("expected indirect rule added: %#v", indirect)
	}
	rule := indirect[0].(map[string]any)
	if rule["text"] != "Splicing mechanism papers that inform RNA engineering." {
		t.Fatalf("unexpected rule: %#v", rule)
	}
	if tags := rule["topics"].([]any); len(tags) != 1 || tags[0] != splicing["id"] {
		t.Fatalf("expected rule tag resolved to new topic id: %#v", rule)
	}
}

func TestPrepareAppliedProfileFromChangesRejectsUnknownTopicLabel(t *testing.T) {
	current := map[string]any{
		"meta": map[string]any{
			"name": "Current", "version": 4, "created_at": "2026-05-01T00:00:00Z",
			"updated_at": "2026-05-12T00:00:00Z", "source_description": "current",
		},
		"scope":           "RNA biology",
		"relevance_rules": map[string]any{"direct": []any{}, "indirect": []any{}, "unrelated": []any{}},
		"topic_taxonomy":  []any{},
		"few_shots":       []any{},
	}
	changes, err := ValidateProposalChanges([]ProposalChange{
		{
			ID: "tag-rule", Section: ProposalSectionDirectRule, Operation: ProposalOperationAdd,
			Summary: "Add rule.", Rationale: "x",
			TextAfter:   []string{"RNA chemistry papers."},
			TopicsAfter: []string{"Ghost Topic"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	changes, err = FinalizeProposalChanges(changes, []string{"tag-rule"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := PrepareAppliedProfileFromChanges(current, changes, time.Now()); err == nil || !strings.Contains(err.Error(), "unknown topic label") {
		t.Fatalf("expected unknown topic label error, got %v", err)
	}
}
