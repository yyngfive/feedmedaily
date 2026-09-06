package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/classifier"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	"github.com/yyngfive/scirssagent/internal/logging"
	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func SelectPaperIDsForScope(settings config.Settings, scope string, limit int) ([]int64, error) {
	return SelectPaperIDsForScopeAt(settings, scope, limit, time.Now())
}

func SelectPaperIDsForScopeAt(settings config.Settings, scope string, limit int, now time.Time) ([]int64, error) {
	// 将管理界面的重分类范围转换成稳定的 paper ids 列表。
	sqliteStore, err := store.Open(settings.DatabasePath)
	if err != nil {
		return nil, err
	}
	defer sqliteStore.Close()

	switch scope {
	case "today":
		localNow := now.In(now.Location())
		start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, localNow.Location())
		return sqliteStore.PaperIDsSeenBetween(start, start.AddDate(0, 0, 1))
	case "count":
		return sqliteStore.RecentPaperIDs(limit)
	case "recent":
		return sqliteStore.RecentPaperIDs(limit)
	case "feedback":
		return sqliteStore.FeedbackPaperIDs()
	case "all":
		return sqliteStore.AllPaperIDs()
	case "unclassified":
		return sqliteStore.UnclassifiedPaperIDs()
	case "topics":
		validIDs, err := validTopicIDSet(settings)
		if err != nil {
			return nil, err
		}
		return sqliteStore.RelatedPaperIDsWithoutTopic(validIDs)
	default:
		return nil, fmt.Errorf("scope must be today, feedback, all, count, unclassified, or topics.")
	}
}

// validTopicIDSet 读取当前 profile 的主题注册表 id 集合，用于补跑选择与防幻觉校验。
func validTopicIDSet(settings config.Settings) (map[string]struct{}, error) {
	current, err := profile.ReadCurrent(settings.ProfilePath)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("No classification profile exists yet.")
	}
	valid := map[string]struct{}{}
	if rawTopics, ok := current["topic_taxonomy"].([]any); ok {
		for _, rawTopic := range rawTopics {
			topic, ok := rawTopic.(map[string]any)
			if !ok {
				continue
			}
			if id, ok := topic["id"].(string); ok && strings.TrimSpace(id) != "" {
				valid[strings.TrimSpace(id)] = struct{}{}
			}
		}
	}
	return valid, nil
}

// CountTopicBackfillPapers 返回 topics 补跑 scope 的目标数量，供触发前的成本预览。
// 尚无 profile 时目标为 0 而不是报错，保持 admin 计数端点在没有 profile 时可用。
func CountTopicBackfillPapers(settings config.Settings) (int, error) {
	sqliteStore, err := store.Open(settings.DatabasePath)
	if err != nil {
		return 0, err
	}
	defer sqliteStore.Close()
	current, err := profile.ReadCurrent(settings.ProfilePath)
	if err != nil {
		return 0, err
	}
	if current == nil {
		return 0, nil
	}
	validIDs, err := validTopicIDSet(settings)
	if err != nil {
		return 0, err
	}
	ids, err := sqliteStore.RelatedPaperIDsWithoutTopic(validIDs)
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

// AssignTopicsPaperIDsContext 对指定论文做 topic-only 补跑：相关性沿用最新
// classification，不重新判定；结果追加写入新的 classification 行。
func AssignTopicsPaperIDsContext(settings config.Settings, paperIDs []int64, ctx context.Context, progress ProgressFunc, collectors ...*llmusage.Collector) (int, error) {
	logging.SetDefaultDir(settings.LogsDir)
	if ctx == nil {
		ctx = context.Background()
	}
	currentProfile, err := profile.ReadCurrent(settings.ProfilePath)
	if err != nil {
		return 0, err
	}
	if currentProfile == nil {
		return 0, fmt.Errorf("No classification profile exists yet.")
	}
	validIDs, err := validTopicIDSet(settings)
	if err != nil {
		return 0, err
	}
	var usage *llmusage.Collector
	if len(collectors) > 0 {
		usage = collectors[0]
	}
	cfg, err := classifierConfig(settings, usage)
	if err != nil {
		return 0, err
	}
	cfg.Context = ctx
	sqliteStore, err := store.Open(settings.DatabasePath)
	if err != nil {
		return 0, err
	}
	defer sqliteStore.Close()

	EmitProgress(progress, PercentProgress(
		"pipeline.classifier.assigning_topics",
		"topics",
		0,
		len(paperIDs),
		fmt.Sprintf("Assigning topics 0/%d.", len(paperIDs)),
	))
	assigned := 0
	batchSize := settings.ClassifierBatchSize
	if batchSize < 1 {
		batchSize = config.DefaultClassifierBatchSize
	}
	for start := 0; start < len(paperIDs); start += batchSize {
		if err := ctx.Err(); err != nil {
			return assigned, err
		}
		end := min(start+batchSize, len(paperIDs))
		batchIDs := paperIDs[start:end]
		papers := make([]store.Paper, 0, len(batchIDs))
		existing := make(map[int64]*store.Classification, len(batchIDs))
		for _, paperID := range batchIDs {
			paper, err := sqliteStore.PaperByID(paperID)
			if err != nil {
				return assigned, err
			}
			if paper == nil {
				continue
			}
			latest, err := sqliteStore.LatestClassification(paperID)
			if err != nil {
				return assigned, err
			}
			if latest == nil {
				// 没有任何分类记录的论文不属于补跑范围。
				continue
			}
			papers = append(papers, *paper)
			existing[paperID] = latest
		}
		if len(papers) == 0 {
			continue
		}
		topics, err := classifier.AssignTopicsBatch(papers, currentProfile, cfg)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return assigned, err
			}
			return assigned, fmt.Errorf("topic assignment batch failed: %w", err)
		}
		for _, paper := range papers {
			latest := existing[paper.ID]
			tags := []string{store.TopicNoneID}
			if topic := topics[paper.ID]; topic != "" {
				if _, ok := validIDs[topic]; ok {
					tags = []string{topic}
				}
			}
			updated := *latest
			updated.TopicTags = tags
			if err := sqliteStore.SaveClassification(paper.ID, updated, time.Now().UTC()); err != nil {
				return assigned, err
			}
			assigned++
		}
		EmitProgress(progress, PercentProgress(
			"pipeline.classifier.assigning_topics",
			"topics",
			assigned,
			len(paperIDs),
			fmt.Sprintf("Assigning topics %d/%d.", assigned, len(paperIDs)),
		))
	}
	return assigned, nil
}

func CountPapers(settings config.Settings) (int, error) {
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return 0, err
	}
	defer sqliteStore.Close()
	return sqliteStore.PaperCount()
}

func CountClassifiedPapers(settings config.Settings) (int, error) {
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return 0, err
	}
	defer sqliteStore.Close()
	return sqliteStore.ClassifiedPaperCount()
}

func CountRecentPaperClassifications(settings config.Settings, limit int) (int, int, error) {
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return 0, 0, err
	}
	defer sqliteStore.Close()
	return sqliteStore.RecentPaperClassificationCounts(limit)
}

func CountTodayPapers(settings config.Settings, now time.Time) (int, int, error) {
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return 0, 0, err
	}
	defer sqliteStore.Close()
	localNow := now.In(now.Location())
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, localNow.Location())
	return sqliteStore.PaperCountsSeenBetween(start, start.AddDate(0, 0, 1))
}

func ReclassifyPaperIDs(settings config.Settings, paperIDs []int64, progress ProgressFunc, collectors ...*llmusage.Collector) (int, error) {
	return ReclassifyPaperIDsContext(settings, paperIDs, context.Background(), progress, collectors...)
}

func ReclassifyPaperIDsContext(settings config.Settings, paperIDs []int64, ctx context.Context, progress ProgressFunc, collectors ...*llmusage.Collector) (int, error) {
	// 用可取消的 Go 原生 metadata + classifier 重分类现有 papers。
	logging.SetDefaultDir(settings.LogsDir)
	if ctx == nil {
		ctx = context.Background()
	}
	currentProfile, err := profile.ReadCurrent(settings.ProfilePath)
	if err != nil {
		return 0, err
	}
	if currentProfile == nil {
		return 0, fmt.Errorf("No classification profile exists yet.")
	}
	var usage *llmusage.Collector
	if len(collectors) > 0 {
		usage = collectors[0]
	}
	cfg, err := classifierConfig(settings, usage)
	if err != nil {
		return 0, err
	}
	cfg.Context = ctx
	sqliteStore, err := store.Open(settings.DatabasePath)
	if err != nil {
		return 0, err
	}
	defer sqliteStore.Close()
	classified, _, err := reclassifyExistingPapersContext(sqliteStore, settings, currentProfile, cfg, paperIDs, ctx, progress)
	return classified, err
}

func RebuildLatestReport(settings config.Settings, progress ProgressFunc) (int, error) {
	// 从 SQLite 重新组装 latest report 摘要，供 admin/proposal apply 链路复用。
	return rebuildLatestReportSummary(settings, progress)
}

func classifierConfig(settings config.Settings, usage *llmusage.Collector) (classifier.LLMConfig, error) {
	model := settings.EffectiveClassifierModel()
	if model.APIKey == "" {
		return classifier.LLMConfig{}, fmt.Errorf("API key is required for classifier model %s.", settings.EffectiveClassifierModelName())
	}
	minMaxTokens := 0
	if model.Thinking == "enabled" && (model.Provider == "deepseek" || model.Provider == "mimo") {
		minMaxTokens = classifier.ThinkingMaxTokensFloor
	}
	return classifier.LLMConfig{
		APIKey:                        model.APIKey,
		Model:                         model.ID,
		BaseURL:                       model.BaseURL,
		Provider:                      model.Provider,
		Thinking:                      model.Thinking,
		ReasoningEffort:               model.ReasoningEffort,
		UseConfiguredProviderControls: true,
		MinMaxTokens:                  minMaxTokens,
		Usage:                         usage,
	}, nil
}
