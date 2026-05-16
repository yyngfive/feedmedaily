package jobs

import (
	"fmt"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/classifier"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
	"github.com/yyngfive/scirssagent/internal/metadata"
	reporting "github.com/yyngfive/scirssagent/internal/report"
	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

type RunOptions struct {
	MaxPapers  int
	Reclassify bool
}

type RunSummary struct {
	Fetched    int      `json:"fetched"`
	Inserted   int      `json:"inserted"`
	Updated    int      `json:"updated"`
	Classified int      `json:"classified"`
	Errors     []string `json:"errors"`
	ReportPath string   `json:"report_path"`
}

var fetchAllFeedsFunc = feeds.FetchAll

// RunOnce executes the end-to-end fetch, ingest, classify, and report pipeline in Go.
func RunOnce(settings config.Settings, opts RunOptions, progress ProgressFunc) (RunSummary, error) {
	if progress != nil {
		progress("pipeline.feeds.fetching", "Fetching RSS feeds.")
	}
	fetchResult, err := fetchAllFeedsFunc(settings.FeedsPath, feeds.FetchOptions{MaxPapers: opts.MaxPapers})
	if err != nil {
		return RunSummary{}, err
	}
	if fetchResult.Fetched == 0 && len(fetchResult.Errors) > 0 {
		return RunSummary{}, fmt.Errorf("%s", strings.Join(fetchResult.Errors, "\n"))
	}

	currentProfile, err := profile.ReadCurrent(settings.ProfilePath)
	if err != nil {
		return RunSummary{}, err
	}
	if currentProfile == nil {
		return RunSummary{}, fmt.Errorf("No classification profile exists yet.")
	}
	cfg, err := classifierConfig(settings)
	if err != nil {
		return RunSummary{}, err
	}

	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return RunSummary{}, err
	}
	defer sqliteStore.Close()

	now := time.Now().UTC()
	touchedIDs := make([]int64, 0, len(fetchResult.Papers))
	inserted := 0
	updated := 0
	for _, paper := range fetchResult.Papers {
		paperID, isNew, err := sqliteStore.UpsertPaper(paper, now)
		if err != nil {
			return RunSummary{}, err
		}
		touchedIDs = append(touchedIDs, paperID)
		if isNew {
			inserted++
		} else {
			updated++
		}
	}

	pendingIDs := touchedIDs
	if !opts.Reclassify {
		pendingIDs, err = sqliteStore.PaperIDsNeedingClassification(touchedIDs)
		if err != nil {
			return RunSummary{}, err
		}
	}
	classified, err := reclassifyExistingPapers(sqliteStore, settings, currentProfile, cfg, pendingIDs, progress)
	if err != nil {
		return RunSummary{}, err
	}
	reportPath, reportCount, err := rebuildLatestReportToDisk(settings, progress)
	if err != nil {
		return RunSummary{}, err
	}
	_ = reportCount
	return RunSummary{
		Fetched:    fetchResult.Fetched,
		Inserted:   inserted,
		Updated:    updated,
		Classified: classified,
		Errors:     fetchResult.Errors,
		ReportPath: reportPath,
	}, nil
}

func reclassifyExistingPapers(sqliteStore *store.Store, settings config.Settings, currentProfile map[string]any, cfg classifier.LLMConfig, paperIDs []int64, progress ProgressFunc) (int, error) {
	if progress != nil {
		progress("pipeline.metadata.enriching", fmt.Sprintf("Getting metadata for %d paper(s).", len(paperIDs)))
	}
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

func rebuildLatestReportToDisk(settings config.Settings, progress ProgressFunc) (string, int, error) {
	if progress != nil {
		progress("pipeline.report.writing", "Publishing the latest report.")
	}
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return "", 0, err
	}
	defer sqliteStore.Close()
	report, err := sqliteStore.BuildLatestReport(time.Now().UTC())
	if err != nil {
		return "", 0, err
	}
	if _, err := reporting.WriteLatestJSON(report, settings.ReportsDir); err != nil {
		return "", 0, err
	}
	indexPath, err := reporting.PublishStaticApp(settings.WebDistDir, settings.ReportsDir, report)
	if err != nil {
		return "", 0, err
	}
	return indexPath, len(report.Papers), nil
}
