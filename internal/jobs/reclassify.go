package jobs

import (
	"fmt"
	"time"

	"github.com/yyngfive/scirssagent/internal/classifier"
	"github.com/yyngfive/scirssagent/internal/config"
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

func ReclassifyPaperIDs(settings config.Settings, paperIDs []int64, progress ProgressFunc) (int, error) {
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
	cfg, err := classifierConfig(settings)
	if err != nil {
		return 0, err
	}
	sqliteStore, err := store.Open(settings.DatabasePath)
	if err != nil {
		return 0, err
	}
	defer sqliteStore.Close()

	enrichedPairs := make([]struct {
		PaperID int64
		Paper   store.Paper
	}, 0, len(paperIDs))
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
		enrichedPairs = append(enrichedPairs, struct {
			PaperID int64
			Paper   store.Paper
		}{PaperID: paperID, Paper: enriched})
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
		batchSize = 10
	}
	classified := 0
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
		papers := make([]store.Paper, 0, len(batch))
		for _, pair := range batch {
			papers = append(papers, pair.Paper)
		}
		results, err := classifier.ClassifyPapers(papers, currentProfile, cfg)
		if err != nil {
			return 0, err
		}
		for index, result := range results {
			if err := sqliteStore.SaveClassification(batch[index].PaperID, result, time.Now().UTC()); err != nil {
				return 0, err
			}
			classified++
		}
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
	return classified, nil
}

func RebuildLatestReport(settings config.Settings, progress ProgressFunc) (int, error) {
	// 从 SQLite 重新组装 latest report 摘要，供 admin/proposal apply 链路复用。
	return rebuildLatestReportSummary(settings, progress)
}

func classifierConfig(settings config.Settings) (classifier.LLMConfig, error) {
	if settings.ClassifierAPIKey == "" {
		return classifier.LLMConfig{}, fmt.Errorf("SCIRSS_CLASSIFIER_API_KEY is required for classification.")
	}
	return classifier.LLMConfig{
		APIKey:   settings.ClassifierAPIKey,
		Model:    settings.ClassifierModel,
		BaseURL:  settings.ClassifierBaseURL,
		Thinking: settings.ClassifierThinking,
	}, nil
}
