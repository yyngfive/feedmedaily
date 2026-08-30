package api

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	"github.com/yyngfive/scirssagent/internal/logging"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

type jobInfo struct {
	ID                       string            `json:"id"`
	JobType                  string            `json:"job_type"`
	Status                   string            `json:"status"`
	MessageKey               string            `json:"message_key,omitempty"`
	Message                  string            `json:"message,omitempty"`
	Error                    string            `json:"error,omitempty"`
	VerificationRequired     bool              `json:"verification_required,omitempty"`
	VerificationTarget       string            `json:"verification_target,omitempty"`
	VerificationFeedURL      string            `json:"verification_feed_url,omitempty"`
	VerificationJournal      string            `json:"verification_journal,omitempty"`
	VerificationHost         string            `json:"verification_host,omitempty"`
	VerificationMethod       string            `json:"verification_method,omitempty"`
	VerificationSessionState string            `json:"verification_session_state,omitempty"`
	Result                   map[string]any    `json:"result,omitempty"`
	LogPath                  string            `json:"log_path,omitempty"`
	WarningCount             int               `json:"warning_count,omitempty"`
	ProgressStage            string            `json:"progress_stage,omitempty"`
	ProgressCurrent          *int              `json:"progress_current,omitempty"`
	ProgressTotal            *int              `json:"progress_total,omitempty"`
	ProgressPercent          *int              `json:"progress_percent,omitempty"`
	ProgressLabel            string            `json:"progress_label,omitempty"`
	ProgressMode             string            `json:"progress_mode,omitempty"`
	CreatedAt                time.Time         `json:"created_at"`
	StartedAt                *time.Time        `json:"started_at,omitempty"`
	FinishedAt               *time.Time        `json:"finished_at,omitempty"`
	LLMUsage                 *llmusage.Summary `json:"llm_usage,omitempty"`
	CancelRequested          bool              `json:"cancel_requested,omitempty"`
}

type jobRegistry struct {
	mu   sync.Mutex
	jobs map[string]jobInfo
}

var (
	apiJobs          = jobRegistry{jobs: map[string]jobInfo{}}
	jobCancellations = struct {
		mu    sync.Mutex
		items map[string]context.CancelFunc
	}{items: map[string]context.CancelFunc{}}
	nowFunc               = time.Now
	backendRunCommandFunc = backendRunCommand
	jobCounter            atomic.Uint64
)

func registerJobCancellation(id string, cancel context.CancelFunc) {
	if strings.TrimSpace(id) == "" || cancel == nil {
		return
	}
	jobCancellations.mu.Lock()
	defer jobCancellations.mu.Unlock()
	jobCancellations.items[id] = cancel
}

func unregisterJobCancellation(id string) {
	jobCancellations.mu.Lock()
	defer jobCancellations.mu.Unlock()
	delete(jobCancellations.items, id)
}

func cancelRegisteredJob(id string) bool {
	jobCancellations.mu.Lock()
	cancel, ok := jobCancellations.items[id]
	jobCancellations.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

// requestJobCancellation asks an active cancellable job to stop and returns its latest state.
func requestJobCancellation(id string) (jobInfo, bool, bool) {
	job, exists := jobByID(id)
	if !exists {
		return jobInfo{}, false, false
	}
	if !isCancellableJobType(job.JobType) || !isActiveJobStatus(job.Status) {
		return job, false, true
	}
	requested := cancelRegisteredJob(id)
	if requested {
		updateJob(id, func(current *jobInfo) {
			if !isActiveJobStatus(current.Status) {
				return
			}
			current.CancelRequested = true
			current.MessageKey = job.JobType + ".cancelling"
			if job.JobType == "reclassify" {
				current.Message = "Stopping reclassification."
			} else {
				current.Message = "Stopping sync."
			}
		})
	}
	job, _ = jobByID(id)
	return job, requested, true
}

type localJobFunc func(context.Context, jobruntime.ProgressFunc, *llmusage.Collector) (map[string]any, error)

// pipelineWork serializes classification work: at most one sync or reclassify
// job may execute at a time. Manual launches take it synchronously (TryLock)
// and reject with 409; apply-launched reclassify jobs wait for it while queued.
// It is a pointer so tests can replace a lock left held by a job parked in
// verification wait without touching that job's goroutine; every acquire
// captures the instance it locked and releases that same instance.
var pipelineWork = new(sync.Mutex)

// tryLockPipeline acquires the current pipeline lock without waiting. On
// success it returns the release func bound to that lock instance.
func tryLockPipeline() (release func(), ok bool) {
	mu := pipelineWork
	if mu.TryLock() {
		return func() { mu.Unlock() }, true
	}
	return nil, false
}

// lockMuBlocking acquires mu, waiting until it is free or ctx is done, and
// returns the release func bound to that lock instance on success.
func lockMuBlocking(mu *sync.Mutex, ctx context.Context) (func(), error) {
	acquired := make(chan struct{})
	go func() {
		mu.Lock()
		close(acquired)
	}()
	select {
	case <-acquired:
		return func() { mu.Unlock() }, nil
	case <-ctx.Done():
		go func() {
			<-acquired
			mu.Unlock()
		}()
		return nil, ctx.Err()
	}
}

// reservePipelineJob reserves a sync job atomically: it reuses the active sync
// job when one exists, otherwise stores the new job while holding the pipeline
// lock so a sync can never start alongside a reclassification. On ok it hands
// back the release func for the acquired lock instance.
func reservePipelineJob(job jobInfo) (reserved jobInfo, reused bool, ok bool, release func()) {
	apiJobs.mu.Lock()
	defer apiJobs.mu.Unlock()
	var active jobInfo
	found := false
	for _, current := range apiJobs.jobs {
		if current.JobType != job.JobType || !isActiveJobStatus(current.Status) {
			continue
		}
		if !found || current.CreatedAt.After(active.CreatedAt) {
			active = current
			found = true
		}
	}
	if found {
		return active, true, true, nil
	}
	release, locked := tryLockPipeline()
	if !locked {
		return job, false, false, nil
	}
	apiJobs.jobs[job.ID] = job
	return job, false, true, release
}

func listJobs() []jobInfo {
	// 返回当前内存中的全部作业，并按创建时间倒序排列。
	apiJobs.mu.Lock()
	defer apiJobs.mu.Unlock()

	jobs := make([]jobInfo, 0, len(apiJobs.jobs))
	for _, job := range apiJobs.jobs {
		jobs = append(jobs, job)
	}
	sortJobsDescending(jobs)
	return jobs
}

func jobByID(id string) (jobInfo, bool) {
	// 根据 job id 查询单个后台作业。
	apiJobs.mu.Lock()
	defer apiJobs.mu.Unlock()
	job, ok := apiJobs.jobs[id]
	return job, ok
}

func launchLocalJob(settings config.Settings, jobType string, queuedMessageKey string, queuedMessage string, runningMessageKey string, runningMessage string, run localJobFunc, wait func(context.Context) (func(), error)) jobInfo {
	// 启动一个纯 Go 本地作业，保持和 legacy bridge 相同的轮询结构。
	// wait 非空时作业先在 queued 状态等待（如 pipeline 锁）；wait 成功返回的
	// release 函数绑定本次获取的锁实例，作业结束时由 goroutine 统一释放。
	job := jobInfo{
		ID:         nextJobID(),
		JobType:    jobType,
		Status:     "queued",
		MessageKey: queuedMessageKey,
		Message:    queuedMessage,
		CreatedAt:  nowFunc().UTC(),
	}
	if path, err := logging.Write(settings.LogsDir, logging.Event{
		Level:      "info",
		Component:  "api.jobs",
		Action:     "queued",
		JobID:      job.ID,
		MessageKey: queuedMessageKey,
		Message:    queuedMessage,
	}); err == nil {
		job.LogPath = path
	}
	storeJob(job)
	ctx, cancel := context.WithCancel(context.Background())
	registerJobCancellation(job.ID, cancel)

	go func() {
		defer func() {
			unregisterJobCancellation(job.ID)
			cancel()
		}()
		if wait != nil {
			release, err := wait(ctx)
			if err != nil {
				finished := nowFunc().UTC()
				usage := llmusage.NewCollector(settings.LLMPricing)
				finishCancelledLocalJob(settings, &job, jobType, nil, usage, finished)
				return
			}
			defer release()
		}
		usage := llmusage.NewCollector(settings.LLMPricing)
		started := nowFunc().UTC()
		logJobEvent(settings.LogsDir, &job, "info", "started", runningMessageKey, runningMessage, "", nil)
		updateJob(job.ID, func(current *jobInfo) {
			current.Status = "running"
			current.MessageKey = runningMessageKey
			current.Message = runningMessage
			current.StartedAt = &started
		})

		progress := func(update jobruntime.ProgressUpdate) {
			logJobEvent(settings.LogsDir, &job, "info", "progress", update.MessageKey, update.Message, "", progressLogData(update))
			updateJob(job.ID, func(current *jobInfo) {
				current.MessageKey = update.MessageKey
				current.Message = update.Message
				current.ProgressStage = update.Stage
				current.ProgressCurrent = update.Current
				current.ProgressTotal = update.Total
				current.ProgressPercent = update.Percent
				current.ProgressLabel = update.Label
				current.ProgressMode = string(update.Mode)
			})
		}
		result, err := run(ctx, progress, usage)
		finished := nowFunc().UTC()
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			finishCancelledLocalJob(settings, &job, jobType, result, usage, finished)
			return
		}
		if err != nil {
			summary := finalizeLLMUsage(settings, job.ID, jobType, "failed", usage, finished)
			logJobEvent(settings.LogsDir, &job, "error", "failed", jobType+".failed", "", err.Error(), nil)
			updateJob(job.ID, func(current *jobInfo) {
				current.Status = "failed"
				current.MessageKey = jobType + ".failed"
				current.Message = ""
				current.Error = err.Error()
				current.LLMUsage = &summary
				clearJobProgress(current)
				current.FinishedAt = &finished
			})
			return
		}

		warningCount := countWarnings(result)
		summary := finalizeLLMUsage(settings, job.ID, jobType, "completed", usage, finished)
		logJobEvent(settings.LogsDir, &job, "info", "completed", jobType+".completed", "Completed.", "", result)
		updateJob(job.ID, func(current *jobInfo) {
			current.Status = "completed"
			current.MessageKey = jobType + ".completed"
			current.Message = summarizeResult(jobType, result) + usageMessage(summary)
			current.Result = result
			current.LLMUsage = &summary
			current.WarningCount = warningCount
			clearJobProgress(current)
			current.FinishedAt = &finished
		})
	}()

	return job
}

func finishCancelledLocalJob(settings config.Settings, job *jobInfo, jobType string, result map[string]any, usage *llmusage.Collector, finished time.Time) {
	message := "Job stopped."
	if jobType == "reclassify" {
		message = "Reclassification stopped."
	}
	summary := finalizeLLMUsage(settings, job.ID, jobType, "cancelled", usage, finished)
	logJobEvent(settings.LogsDir, job, "info", "cancelled", jobType+".cancelled", message, "", result)
	updateJob(job.ID, func(current *jobInfo) {
		current.Status = "cancelled"
		current.MessageKey = jobType + ".cancelled"
		current.Message = message
		current.Error = ""
		current.Result = result
		current.LLMUsage = &summary
		current.WarningCount = countWarnings(result)
		clearJobProgress(current)
		current.FinishedAt = &finished
		current.CancelRequested = true
	})
}

func finalizeLLMUsage(settings config.Settings, jobID string, jobType string, status string, collector *llmusage.Collector, finished time.Time) llmusage.Summary {
	summary := collector.Summary()
	if len(summary.Models) == 0 {
		switch jobType {
		case "sync", "reclassify", "model-test":
			summary.Models = []string{settings.EffectiveClassifierModelName()}
		case "profile-bootstrap", "profile-proposal":
			summary.Models = []string{settings.ProfileModel}
		}
	}
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err == nil {
		err = sqliteStore.SaveLLMUsage(jobID, jobType, status, summary, finished)
		_ = sqliteStore.Close()
	}
	if err != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level: "warning", Component: "api.jobs", Action: "llm_usage_persist_failed", JobID: jobID,
			Message: "Could not persist LLM usage; the job result is unchanged.", Error: err.Error(),
		})
	}
	return summary
}

func usageMessage(summary llmusage.Summary) string {
	if summary.EstimatedCostCNY == nil {
		return " LLM cost unavailable."
	}
	return fmt.Sprintf(" Estimated LLM cost ¥%s.", *summary.EstimatedCostCNY)
}

func nextJobID() string {
	// 用时间戳加递增计数器生成稳定唯一的本地 job id。
	return fmt.Sprintf("%d-%d", nowFunc().UnixNano(), jobCounter.Add(1))
}

func storeJob(job jobInfo) {
	// 保存或更新作业，供轮询接口读取。
	apiJobs.mu.Lock()
	defer apiJobs.mu.Unlock()
	apiJobs.jobs[job.ID] = job
}

func isActiveJobStatus(status string) bool {
	switch status {
	case "queued", "running", "waiting_for_user":
		return true
	default:
		return false
	}
}

func isCancellableJobType(jobType string) bool {
	return jobType == "sync" || jobType == "reclassify"
}

func updateJob(id string, apply func(*jobInfo)) {
	// 对已有作业做原子更新，避免状态竞争。
	apiJobs.mu.Lock()
	defer apiJobs.mu.Unlock()
	job := apiJobs.jobs[id]
	apply(&job)
	apiJobs.jobs[id] = job
}

func logJobEvent(logsDir string, job *jobInfo, level string, action string, messageKey string, message string, errText string, data map[string]any) {
	if logsDir == "" {
		return
	}
	path, err := logging.Write(logsDir, logging.Event{
		Level:      level,
		Component:  "api.jobs",
		Action:     action,
		JobID:      job.ID,
		MessageKey: messageKey,
		Message:    message,
		Error:      errText,
		Data:       data,
	})
	if err == nil && job.LogPath == "" {
		job.LogPath = path
		updateJob(job.ID, func(current *jobInfo) {
			current.LogPath = path
		})
	}
}

func progressLogData(update jobruntime.ProgressUpdate) map[string]any {
	data := map[string]any{}
	if update.Stage != "" {
		data["progress_stage"] = update.Stage
	}
	if update.Current != nil {
		data["progress_current"] = *update.Current
	}
	if update.Total != nil {
		data["progress_total"] = *update.Total
	}
	if update.Percent != nil {
		data["progress_percent"] = *update.Percent
	}
	if update.Label != "" {
		data["progress_label"] = update.Label
	}
	if update.Mode != "" {
		data["progress_mode"] = string(update.Mode)
	}
	if len(data) == 0 {
		return nil
	}
	return data
}

func clearJobProgress(job *jobInfo) {
	job.ProgressStage = ""
	job.ProgressCurrent = nil
	job.ProgressTotal = nil
	job.ProgressPercent = nil
	job.ProgressLabel = ""
	job.ProgressMode = ""
}

func countWarnings(result map[string]any) int {
	errorsValue, ok := result["errors"]
	if !ok {
		return 0
	}
	items, ok := errorsValue.([]string)
	if ok {
		return len(items)
	}
	rawItems, ok := errorsValue.([]any)
	if !ok {
		return 0
	}
	return len(rawItems)
}

func summarizeResult(jobType string, result map[string]any) string {
	switch jobType {
	case "sync":
		warnings := countWarnings(result)
		message := "Sync completed."
		if warnings > 0 {
			message = fmt.Sprintf("Sync completed with %d fetch warning(s).", warnings)
		}
		return fmt.Sprintf(
			"%s fetched=%v inserted=%v updated=%v classified=%v warnings=%d.",
			message,
			result["fetched"],
			result["inserted"],
			result["updated"],
			result["classified"],
			warnings,
		)
	case "reclassify":
		return fmt.Sprintf(
			"Reclassify completed. scope=%v reclassified=%v report_papers=%v.",
			result["scope"],
			result["reclassified"],
			result["report_papers"],
		)
	case "profile-bootstrap":
		return fmt.Sprintf("Initial profile proposal completed. proposal_id=%v.", result["proposal_id"])
	case "profile-proposal":
		if accepted, ok := result["accepted"].(bool); ok && !accepted {
			summary := strings.TrimSpace(fmt.Sprint(result["summary"]))
			if summary == "" || summary == "<nil>" {
				summary = "See validator details for required fixes."
			}
			return fmt.Sprintf("Profile proposal rejected by safety review. %s", summary)
		}
		return fmt.Sprintf("Profile proposal completed. proposal_id=%v.", result["proposal_id"])
	default:
		return "Completed."
	}
}

func sortJobsDescending(jobs []jobInfo) {
	// 简单地按创建时间倒序排序，供前端轮询列表使用。
	for i := 0; i < len(jobs); i++ {
		for j := i + 1; j < len(jobs); j++ {
			if jobs[j].CreatedAt.After(jobs[i].CreatedAt) {
				jobs[i], jobs[j] = jobs[j], jobs[i]
			}
		}
	}
}

func backendRunCommand(settings config.Settings) ([]string, error) {
	return backendRunCommandForPlatform(settings, goruntime.GOOS)
}

func backendRunCommandForPlatform(settings config.Settings, goos string) ([]string, error) {
	// 在 Linux source mode 下给设置页返回推荐的 cron/helper 脚本命令。
	if goos != "linux" || settings.Mode != appruntime.ModeSource {
		return nil, nil
	}
	return []string{"bash", filepath.Join(settings.RootDir, "tools", "feedmedaily.sh"), "sync"}, nil
}
