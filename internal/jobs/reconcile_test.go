package jobs

import (
	"testing"

	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestEvaluateCorrection(t *testing.T) {
	topicLabels := map[string]string{"topic-alpha": "Alpha", "topic-beta": "Beta"}
	status := func(record store.FeedbackRecord, classification store.Classification) CorrectionStatus {
		result, _ := evaluateCorrection(record, classification, topicLabels)
		return result
	}

	tests := []struct {
		name           string
		record         store.FeedbackRecord
		classification store.Classification
		wantFulfilled  bool
	}{
		{
			name: "relevance correction fulfilled",
			record: store.FeedbackRecord{
				ID: 1, PaperID: 11,
				OriginalRelevance: "indirect", CorrectedRelevance: "direct",
			},
			classification: store.Classification{Relevance: "direct", TopicTags: []string{store.TopicNoneID}},
			wantFulfilled:  true,
		},
		{
			name: "relevance correction unfulfilled",
			record: store.FeedbackRecord{
				ID: 2, PaperID: 12,
				OriginalRelevance: "indirect", CorrectedRelevance: "direct",
			},
			classification: store.Classification{Relevance: "indirect", TopicTags: []string{store.TopicNoneID}},
			wantFulfilled:  false,
		},
		{
			name: "topic correction fulfilled",
			record: store.FeedbackRecord{
				ID: 3, PaperID: 13,
				OriginalRelevance: "direct", CorrectedRelevance: "direct",
				OriginalTopic: strPtr("topic-alpha"), CorrectedTopic: strPtr("topic-beta"),
			},
			classification: store.Classification{Relevance: "direct", TopicTags: []string{"topic-beta"}},
			wantFulfilled:  true,
		},
		{
			name: "topic correction unfulfilled while relevance matches",
			record: store.FeedbackRecord{
				ID: 4, PaperID: 14,
				OriginalRelevance: "direct", CorrectedRelevance: "direct",
				OriginalTopic: strPtr("topic-alpha"), CorrectedTopic: strPtr("topic-beta"),
			},
			classification: store.Classification{Relevance: "direct", TopicTags: []string{"topic-alpha"}},
			wantFulfilled:  false,
		},
		{
			name: "no-topic correction fulfilled against empty legacy tags",
			record: store.FeedbackRecord{
				ID: 5, PaperID: 15,
				OriginalRelevance: "direct", CorrectedRelevance: "direct",
				OriginalTopic: strPtr("topic-alpha"), CorrectedTopic: strPtr(store.TopicNoneID),
			},
			classification: store.Classification{Relevance: "direct", TopicTags: []string{}},
			wantFulfilled:  true,
		},
		{
			name: "no-topic correction unfulfilled while a topic is assigned",
			record: store.FeedbackRecord{
				ID: 6, PaperID: 16,
				OriginalRelevance: "direct", CorrectedRelevance: "direct",
				OriginalTopic: strPtr("topic-alpha"), CorrectedTopic: strPtr(store.TopicNoneID),
			},
			classification: store.Classification{Relevance: "direct", TopicTags: []string{"topic-alpha"}},
			wantFulfilled:  false,
		},
		{
			name: "topic opinion fails when relevance flipped to unrelated",
			record: store.FeedbackRecord{
				ID: 7, PaperID: 17,
				OriginalRelevance: "direct", CorrectedRelevance: "direct",
				OriginalTopic: strPtr("topic-alpha"), CorrectedTopic: strPtr("topic-beta"),
			},
			classification: store.Classification{Relevance: "unrelated", TopicTags: []string{}},
			wantFulfilled:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, fulfilled := evaluateCorrection(tt.record, tt.classification, topicLabels)
			if fulfilled != tt.wantFulfilled {
				t.Fatalf("fulfilled = %v, want %v", fulfilled, tt.wantFulfilled)
			}
		})
	}

	t.Run("unfulfilled status carries display labels", func(t *testing.T) {
		status := status(store.FeedbackRecord{
			ID: 8, PaperID: 18,
			OriginalRelevance: "direct", CorrectedRelevance: "direct",
			OriginalTopic: strPtr("topic-alpha"), CorrectedTopic: strPtr("topic-beta"),
		}, store.Classification{Relevance: "direct", TopicTags: []string{profile.TopicNoneID}})
		if status.CurrentTopic != "none" {
			t.Fatalf("current topic label = %q, want %q", status.CurrentTopic, "none")
		}
		if status.OriginalTopic == nil || *status.OriginalTopic != "Alpha" {
			t.Fatalf("original topic label = %v, want Alpha", status.OriginalTopic)
		}
		if status.CorrectedTopic == nil || *status.CorrectedTopic != "Beta" {
			t.Fatalf("corrected topic label = %v, want Beta", status.CorrectedTopic)
		}
	})

	t.Run("orphan topic id degrades to deleted-topic label", func(t *testing.T) {
		status := status(store.FeedbackRecord{
			ID: 9, PaperID: 19,
			OriginalRelevance: "direct", CorrectedRelevance: "direct",
			CorrectedTopic: strPtr("topic-beta"),
		}, store.Classification{Relevance: "direct", TopicTags: []string{"topic-gone"}})
		if status.CurrentTopic != "deleted topic topic-gone" {
			t.Fatalf("current topic label = %q", status.CurrentTopic)
		}
	})

	t.Run("nil corrected topic means no topic opinion", func(t *testing.T) {
		_, fulfilled := evaluateCorrection(store.FeedbackRecord{
			ID: 10, PaperID: 20,
			OriginalRelevance: "indirect", CorrectedRelevance: "direct",
		}, store.Classification{Relevance: "direct", TopicTags: []string{"topic-alpha"}}, topicLabels)
		if !fulfilled {
			t.Fatal("relevance-only correction should be fulfilled regardless of topic")
		}
	})
}

func strPtr(value string) *string {
	return &value
}
