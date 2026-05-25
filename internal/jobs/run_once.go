package jobs

import (
	"fmt"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/classifier"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
	"github.com/yyngfive/scirssagent/internal/logging"
	"github.com/yyngfive/scirssagent/internal/metadata"
	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

type RunOptions struct {
	MaxPapers         int
	Reclassify        bool
	FeedBodyOverrides map[string][]byte
	SkippedFeeds      map[string]string
}

type RunSummary struct {
	Fetched    int      `json:"fetched"`
	Inserted   int      `json:"inserted"`
	Updated    int      `json:"updated"`
	Classified int      `json:"classified"`
	Errors     []string `json:"errors"`
}

type VerificationRequiredError struct {
	Requests []feeds.VerificationRequest
}

func (e *VerificationRequiredError) Error() string {
	if len(e.Requests) == 0 {
		return "manual verification required"
	}
	return fmt.Sprintf("manual verification required for %s", e.Requests[0].URL)
}

var fetchAllFeedsFunc = feeds.FetchAll

// RunSync executes the end-to-end sync pipeline in Go.
func RunSync(settings config.Settings, opts RunOptions, progress ProgressFunc) (RunSummary, error) {
	logging.SetDefaultDir(settings.LogsDir)
	if progress != nil {
		progress("pipeline.feeds.fetching", "Fetching RSS feeds.")
	}
	fetchResult, err := fetchAllFeedsFunc(settings.FeedsPath, feeds.FetchOptions{
		MaxPapers:      opts.MaxPapers,
		OverrideBodies: opts.FeedBodyOverrides,
		SkippedFeeds:   opts.SkippedFeeds,
	})
	if err != nil {
		return RunSummary{}, err
	}
	if len(fetchResult.VerificationRequests) > 0 {
		return RunSummary{}, &VerificationRequiredError{Requests: fetchResult.VerificationRequests}
	}
	if fetchResult.Fetched == 0 {
		nonSkippedErrors := filterNonSkippedErrors(fetchResult.Errors, opts.SkippedFeeds)
		if len(nonSkippedErrors) > 0 {
			return RunSummary{}, fmt.Errorf("%s", strings.Join(nonSkippedErrors, "\n"))
		}
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
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "jobs.sync",
		Action:    "ingest_completed",
		Message:   fmt.Sprintf("Inserted %d new papers; %d papers need classification", inserted, len(pendingIDs)),
		Data: map[string]any{
			"fetched":     fetchResult.Fetched,
			"inserted":    inserted,
			"updated":     updated,
			"pending_ids": len(pendingIDs),
			"warnings":    len(fetchResult.Errors),
		},
	})
	classified, err := reclassifyExistingPapers(sqliteStore, settings, currentProfile, cfg, pendingIDs, progress)
	if err != nil {
		return RunSummary{}, err
	}
	reportCount, err := rebuildLatestReportSummary(settings, progress)
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
	}, nil
}

func filterNonSkippedErrors(errors []string, skippedFeeds map[string]string) []string {
	if len(errors) == 0 || len(skippedFeeds) == 0 {
		return append([]string(nil), errors...)
	}
	filtered := make([]string, 0, len(errors))
	for _, item := range errors {
		skipped := false
		for feedURL := range skippedFeeds {
			if strings.HasPrefix(item, feedURL+": ") {
				skipped = true
				break
			}
		}
		if !skipped {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func reclassifyExistingPapers(sqliteStore *store.Store, settings config.Settings, currentProfile map[string]any, cfg classifier.LLMConfig, paperIDs []int64, progress ProgressFunc) (int, error) {
	logging.SetDefaultDir(settings.LogsDir)
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
		batchNumber := start/batchSize + 1
		totalBatches := (len(enrichedPairs) + batchSize - 1) / batchSize
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "info",
			Component: "jobs.classifier",
			Action:    "batch_started",
			Message:   fmt.Sprintf("Classifying batch %d/%d (%d paper(s))", batchNumber, totalBatches, len(batch)),
			Data: map[string]any{
				"batch":         batchNumber,
				"total_batches": totalBatches,
				"batch_size":    len(batch),
			},
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
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "info",
			Component: "jobs.classifier",
			Action:    "batch_completed",
			Message:   fmt.Sprintf("Finished batch %d/%d", batchNumber, totalBatches),
			Data: map[string]any{
				"batch":              batchNumber,
				"total_batches":      totalBatches,
				"classified_running": classified,
			},
		})
	}
	return classified, nil
}

func rebuildLatestReportSummary(settings config.Settings, progress ProgressFunc) (int, error) {
	logging.SetDefaultDir(settings.LogsDir)
	if progress != nil {
		progress("pipeline.report.refreshing", "Refreshing the latest report from SQLite.")
	}
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return 0, err
	}
	defer sqliteStore.Close()
	report, err := sqliteStore.BuildLatestReport(time.Now().UTC())
	if err != nil {
		return 0, err
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "jobs.report",
		Action:    "report_refreshed",
		Message:   "Latest report assembled from SQLite.",
		Data: map[string]any{
			"report_papers": len(report.Papers),
		},
	})
	return len(report.Papers), nil
}
