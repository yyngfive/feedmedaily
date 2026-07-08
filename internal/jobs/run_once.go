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
	SelectedFeedURLs  []string
	FeedBodyOverrides map[string][]byte
	SkippedFeeds      map[string]string
	VerifyFeedHost    feeds.VerifyHostFunc
}

type RunSummary struct {
	Fetched    int      `json:"fetched"`
	Inserted   int      `json:"inserted"`
	Updated    int      `json:"updated"`
	Classified int      `json:"classified"`
	Errors     []string `json:"errors"`
}

type classificationPaperPair struct {
	PaperID int64
	Paper   store.Paper
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
	fetchResult, err := fetchAllFeedsFunc(settings.FeedsPath, feeds.FetchOptions{
		MaxPapers:        opts.MaxPapers,
		SelectedFeedURLs: opts.SelectedFeedURLs,
		OverrideBodies:   opts.FeedBodyOverrides,
		BodyCache:        opts.FeedBodyOverrides,
		SkippedFeeds:     opts.SkippedFeeds,
		VerifyHost:       opts.VerifyFeedHost,
		Progress: func(current int, total int, label string) {
			message := fmt.Sprintf("Fetching feeds %d/%d.", current, total)
			if current > 0 && label != "" {
				message = fmt.Sprintf("Fetching feed %d/%d: %s.", current, total, label)
			}
			EmitProgress(progress, ItemProgress("pipeline.feeds.fetching", "fetch", current, total, label, message))
		},
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
	classified, classificationWarnings, err := reclassifyExistingPapers(sqliteStore, settings, currentProfile, cfg, pendingIDs, progress)
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
		Errors:     append(fetchResult.Errors, classificationWarnings...),
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

func reclassifyExistingPapers(sqliteStore *store.Store, settings config.Settings, currentProfile map[string]any, cfg classifier.LLMConfig, paperIDs []int64, progress ProgressFunc) (int, []string, error) {
	logging.SetDefaultDir(settings.LogsDir)
	EmitProgress(progress, PercentProgress("pipeline.metadata.enriching", "metadata", 0, len(paperIDs), metadataProgressMessage(0, len(paperIDs))))
	enrichedPairs := make([]classificationPaperPair, 0, len(paperIDs))
	now := time.Now().UTC()
	for _, paperID := range paperIDs {
		paper, err := sqliteStore.PaperByID(paperID)
		if err != nil {
			return 0, nil, err
		}
		if paper == nil {
			continue
		}
		enriched := metadata.EnrichPaper(*paper)
		enriched.ID = paperID
		if _, _, err := sqliteStore.UpsertPaper(enriched, now); err != nil {
			return 0, nil, err
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
		return 0, nil, nil
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
	classificationWarnings := []string{}
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
		batchClassified, batchWarnings, err := classifyAndSaveBatch(sqliteStore, "jobs.classifier", batch, currentProfile, cfg)
		if err != nil {
			return 0, classificationWarnings, err
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
	if classified == 0 && len(classificationWarnings) > 0 {
		return classified, classificationWarnings, fmt.Errorf("all classification attempts failed: %s", summarizeClassificationWarnings(classificationWarnings))
	}
	return classified, classificationWarnings, nil
}

func classifyAndSaveBatch(sqliteStore *store.Store, component string, batch []classificationPaperPair, currentProfile map[string]any, cfg classifier.LLMConfig) (int, []string, error) {
	papers := make([]store.Paper, 0, len(batch))
	for _, pair := range batch {
		papers = append(papers, pair.Paper)
	}
	results, err := classifier.ClassifyPapers(papers, currentProfile, cfg)
	if err == nil {
		for index, result := range results {
			if err := sqliteStore.SaveClassification(batch[index].PaperID, result, time.Now().UTC()); err != nil {
				return 0, nil, err
			}
		}
		return len(results), nil, nil
	}
	if len(batch) == 1 {
		warning := classificationWarning(batch[0].PaperID, err)
		logClassificationWarning(component, "single_failed", warning, err, 1)
		return 0, []string{warning}, nil
	}
	logClassificationWarning(component, "batch_failed_degrading", "Classifier batch failed; retrying papers one by one.", err, len(batch))
	classified := 0
	warnings := []string{}
	for _, pair := range batch {
		results, err := classifier.ClassifyPapers([]store.Paper{pair.Paper}, currentProfile, cfg)
		if err != nil {
			warning := classificationWarning(pair.PaperID, err)
			warnings = append(warnings, warning)
			logClassificationWarning(component, "single_failed", warning, err, 1)
			continue
		}
		if len(results) != 1 {
			warning := fmt.Sprintf("classification failed for paper %d: single-paper response returned %d results", pair.PaperID, len(results))
			warnings = append(warnings, warning)
			logClassificationWarning(component, "single_result_mismatch", warning, nil, 1)
			continue
		}
		if err := sqliteStore.SaveClassification(pair.PaperID, results[0], time.Now().UTC()); err != nil {
			return classified, warnings, err
		}
		classified++
	}
	return classified, warnings, nil
}

func classificationWarning(paperID int64, err error) string {
	return fmt.Sprintf("classification failed for paper %d: %s", paperID, err.Error())
}

func logClassificationWarning(component string, action string, message string, err error, batchSize int) {
	errorMessage := ""
	if err != nil {
		errorMessage = err.Error()
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "warning",
		Component: component,
		Action:    action,
		Message:   message,
		Error:     errorMessage,
		Data: map[string]any{
			"batch_size": batchSize,
		},
	})
}

func summarizeClassificationWarnings(warnings []string) string {
	if len(warnings) <= 3 {
		return strings.Join(warnings, "; ")
	}
	return strings.Join(warnings[:3], "; ") + fmt.Sprintf("; and %d more", len(warnings)-3)
}

func rebuildLatestReportSummary(settings config.Settings, progress ProgressFunc) (int, error) {
	logging.SetDefaultDir(settings.LogsDir)
	EmitProgress(progress, ProgressUpdate{
		MessageKey: "pipeline.report.refreshing",
		Message:    "Refreshing the latest report from SQLite.",
		Stage:      "report",
	})
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
