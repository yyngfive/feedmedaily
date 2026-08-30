package jobs

import (
	"context"
	"fmt"
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
	default:
		return nil, fmt.Errorf("scope must be today, feedback, all, count, or unclassified.")
	}
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
