package jobs

import (
	"fmt"
	"time"

	"github.com/yyngfive/scirssagent/internal/classifier"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/metadata"
	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

type ProgressFunc func(string, string)

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
	if progress != nil {
		progress("pipeline.metadata.enriching", fmt.Sprintf("Getting metadata for %d paper(s).", len(paperIDs)))
	}
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
	}
	if len(enrichedPairs) == 0 {
		return 0, nil
	}
	if progress != nil {
		progress("pipeline.classifier.classifying", fmt.Sprintf("Classifying %d paper(s).", len(enrichedPairs)))
	}
	batchSize := settings.ClassifierBatchSize
	if batchSize < 1 {
		batchSize = 10
	}
	classified := 0
	for start := 0; start < len(enrichedPairs); start += batchSize {
		end := min(start+batchSize, len(enrichedPairs))
		batch := enrichedPairs[start:end]
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
	}
	return classified, nil
}

func RebuildLatestReport(settings config.Settings, progress ProgressFunc) (int, error) {
	// 触发一次本地 report 重建并发布到磁盘，保持 admin/proposal apply 后链路一致。
	_, count, err := rebuildLatestReportToDisk(settings, progress)
	return count, err
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
