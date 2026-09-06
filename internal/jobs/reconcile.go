package jobs

import (
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

// CorrectionStatus 是一条 feedback 纠正的对账结果：纠正值与最新分类不一致时进入未落实清单。
// 主题字段存展示 label（哨兵 none 显示 "none"，孤儿 id 降级为 "deleted topic {id}"），
// 由后端解析，前端无需持有主题注册表。
type CorrectionStatus struct {
	FeedbackID         int64   `json:"feedback_id"`
	PaperID            int64   `json:"paper_id"`
	PaperTitle         string  `json:"paper_title"`
	OriginalRelevance  string  `json:"original_relevance"`
	CorrectedRelevance string  `json:"corrected_relevance"`
	CurrentRelevance   string  `json:"current_relevance"`
	OriginalTopic      *string `json:"original_topic"`
	CorrectedTopic     *string `json:"corrected_topic"`
	CurrentTopic       string  `json:"current_topic"`
}

// ReconcileResult 汇总一次重分类后 feedback 纠正的落实情况。Checked 为 0 时
// 调用方不应把结果放进 job result（与本次重分类无关的作业不打扰用户）。
type ReconcileResult struct {
	Checked     int                `json:"checked"`
	Fulfilled   int                `json:"fulfilled"`
	Unfulfilled []CorrectionStatus `json:"unfulfilled"`
}

// ReconcileFeedback 对一次重分类涉及论文上的 feedback 纠正与各自最新分类做对账。
// feedbackIDs 是显式指定的记录（apply-proposal 路径在重分类前刚关闭的那批）；
// 其余取这些论文上仍 open 的记录，按 id 去重。"已消费"（feedback 关闭）与
// "已生效"（分类等于纠正值）是两个谓词，这里回答的是后者。
func ReconcileFeedback(settings config.Settings, paperIDs []int64, feedbackIDs []int64) (ReconcileResult, error) {
	result := ReconcileResult{Unfulfilled: []CorrectionStatus{}}
	sqliteStore, err := store.Open(settings.DatabasePath)
	if err != nil {
		return result, err
	}
	defer sqliteStore.Close()

	seen := map[int64]struct{}{}
	records := make([]store.FeedbackRecord, 0, len(feedbackIDs))
	if len(feedbackIDs) > 0 {
		byIDs, err := sqliteStore.FeedbackByIDs(feedbackIDs)
		if err != nil {
			return result, err
		}
		for _, record := range byIDs {
			if _, ok := seen[record.ID]; ok {
				continue
			}
			seen[record.ID] = struct{}{}
			records = append(records, record)
		}
	}
	open, err := sqliteStore.OpenFeedbackForPapers(paperIDs)
	if err != nil {
		return result, err
	}
	for _, record := range open {
		if _, ok := seen[record.ID]; ok {
			continue
		}
		seen[record.ID] = struct{}{}
		records = append(records, record)
	}
	if len(records) == 0 {
		return result, nil
	}

	current, err := profile.ReadCurrent(settings.ProfilePath)
	if err != nil {
		return result, err
	}
	topicLabels := map[string]string{}
	if current != nil {
		if rawTopics, ok := current["topic_taxonomy"].([]any); ok {
			for _, rawTopic := range rawTopics {
				topic, ok := rawTopic.(map[string]any)
				if !ok {
					continue
				}
				id, _ := topic["id"].(string)
				label, _ := topic["label"].(string)
				if id != "" && label != "" {
					topicLabels[id] = label
				}
			}
		}
	}

	for _, record := range records {
		classification, err := sqliteStore.LatestClassification(record.PaperID)
		if err != nil {
			return result, err
		}
		if classification == nil {
			continue
		}
		result.Checked++
		if status, fulfilled := evaluateCorrection(record, *classification, topicLabels); !fulfilled {
			result.Unfulfilled = append(result.Unfulfilled, status)
		} else {
			result.Fulfilled++
		}
	}
	return result, nil
}

// evaluateCorrection 比较一条 feedback 与论文最新分类：相关性必须相等；记录了
// 主题意见（CorrectedTopic 非 nil）时主题也必须相等。topic_tags 空数组按哨兵
// none 处理（legacy 行与 unrelated 论文），与分类器写入口径一致。
func evaluateCorrection(record store.FeedbackRecord, classification store.Classification, topicLabels map[string]string) (CorrectionStatus, bool) {
	currentTopicID := store.TopicNoneID
	if len(classification.TopicTags) > 0 {
		currentTopicID = classification.TopicTags[0]
	}
	relevanceFulfilled := classification.Relevance == record.CorrectedRelevance
	topicFulfilled := record.CorrectedTopic == nil || currentTopicID == *record.CorrectedTopic
	status := CorrectionStatus{
		FeedbackID:         record.ID,
		PaperID:            record.PaperID,
		PaperTitle:         record.PaperTitle,
		OriginalRelevance:  record.OriginalRelevance,
		CorrectedRelevance: record.CorrectedRelevance,
		CurrentRelevance:   classification.Relevance,
		CurrentTopic:       displayTopicValue(currentTopicID, topicLabels),
	}
	if record.OriginalTopic != nil {
		label := displayTopicValue(*record.OriginalTopic, topicLabels)
		status.OriginalTopic = &label
	}
	if record.CorrectedTopic != nil {
		label := displayTopicValue(*record.CorrectedTopic, topicLabels)
		status.CorrectedTopic = &label
	}
	return status, relevanceFulfilled && topicFulfilled
}
