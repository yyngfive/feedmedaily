package profile

import (
	"os"
	"path/filepath"
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
	if len(taxonomy) != 1 {
		t.Fatalf("unexpected taxonomy: %#v", taxonomy)
	}
	fewShots := payload["few_shots"].([]any)
	if len(fewShots) != 2 {
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
}
