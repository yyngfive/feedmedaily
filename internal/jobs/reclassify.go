package jobs

import (
	"fmt"
	"time"

	"github.com/yyngfive/scirssagent/internal/classifier"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	"github.com/yyngfive/scirssagent/internal/logging"
	"github.com/yyngfive/scirssagent/internal/metadata"
	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func SelectPaperIDsForScope(settings config.Settings, scope string, limit int) ([]int64, error) {
	// 复刻 Python reclassify scope 到 paper ids 的选择逻辑。
	sqliteStore, err := store.Open(settings.DatabasePath)
	if err != nil {
		return nil, err
	}
	defer sqliteStore.Close()

	switch scope {
	case "recent":
		return sqliteStore.RecentPaperIDs(limit)
	case "feedback":
		return sqliteStore.FeedbackPaperIDs()
	case "all":
		return sqliteStore.AllPaperIDs()
	default:
		return nil, fmt.Errorf("scope must be recent, feedback, or all.")
	}
}

func ReclassifyPaperIDs(settings config.Settings, paperIDs []int64, progress ProgressFunc, collectors ...*llmusage.Collector) (int, error) {
	// 用 Go 原生 metadata + classifier 重分类现有 papers。
	logging.SetDefaultDir(settings.LogsDir)
	EmitProgress(progress, PercentProgress("pipeline.metadata.enriching", "metadata", 0, len(paperIDs), metadataProgressMessage(0, len(paperIDs))))
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
	sqliteStore, err := store.Open(settings.DatabasePath)
	if err != nil {
		return 0, err
	}
	defer sqliteStore.Close()

	enrichedPairs := make([]classificationPaperPair, 0, len(paperIDs))
	now := time.Now().UTC()
	for _, paperID := range paperIDs {
		paper, err := sqliteStore.PaperByID(paperID)
		if err != nil {
			return 0, err
		}
		if paper == nil {
			continue
		}
		enriched := metadata.EnrichPaper(*paper)
		enriched.ID = paperID
		if _, _, err := sqliteStore.UpsertPaper(enriched, now); err != nil {
			return 0, err
		}
		enrichedPairs = append(enrichedPairs, classificationPaperPair{PaperID: paperID, Paper: enriched})
		EmitProgress(progress, PercentProgress(
			"pipeline.metadata.enriching",
			"metadata",
			len(enrichedPairs),
			len(paperIDs),
			metadataProgressMessage(len(enrichedPairs), len(paperIDs)),
		))
	}
	if len(enrichedPairs) == 0 {
		return 0, nil
	}
	EmitProgress(progress, PercentProgress(
		"pipeline.classifier.classifying",
		"classification",
		0,
		len(enrichedPairs),
		classificationProgressMessage(0, len(enrichedPairs)),
	))
	batchSize := settings.ClassifierBatchSize
	if batchSize < 1 {
		batchSize = config.DefaultClassifierBatchSize
	}
	classified := 0
	classificationWarnings := []string{}
	for start := 0; start < len(enrichedPairs); start += batchSize {
		end := min(start+batchSize, len(enrichedPairs))
		batch := enrichedPairs[start:end]
		batchNumber := start/batchSize + 1
		totalBatches := (len(enrichedPairs) + batchSize - 1) / batchSize
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "info",
			Component: "jobs.reclassify",
			Action:    "batch_started",
			Message:   fmt.Sprintf("Classifying batch %d/%d (%d paper(s))", batchNumber, totalBatches, len(batch)),
			Data:      map[string]any{"batch": batchNumber, "total_batches": totalBatches, "batch_size": len(batch)},
		})
		batchClassified, batchWarnings, err := classifyAndSaveBatch(sqliteStore, "jobs.reclassify", batch, currentProfile, cfg)
		if err != nil {
			return 0, err
		}
		classified += batchClassified
		classificationWarnings = append(classificationWarnings, batchWarnings...)
		EmitProgress(progress, PercentProgress(
			"pipeline.classifier.classifying",
			"classification",
			classified,
			len(enrichedPairs),
			classificationProgressMessage(classified, len(enrichedPairs)),
		))
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "info",
			Component: "jobs.reclassify",
			Action:    "batch_completed",
			Message:   fmt.Sprintf("Finished batch %d/%d", batchNumber, totalBatches),
			Data:      map[string]any{"batch": batchNumber, "total_batches": totalBatches, "classified_running": classified},
		})
	}
	if classified == 0 && len(classificationWarnings) > 0 {
		return classified, fmt.Errorf("all classification attempts failed: %s", summarizeClassificationWarnings(classificationWarnings))
	}
	return classified, nil
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
