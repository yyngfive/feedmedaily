package api

import (
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
}

type jobRegistry struct {
	mu   sync.Mutex
	jobs map[string]jobInfo
}

var (
	apiJobs               = jobRegistry{jobs: map[string]jobInfo{}}
	nowFunc               = time.Now
	backendRunCommandFunc = backendRunCommand
	jobCounter            atomic.Uint64
)

type localJobFunc func(progress jobruntime.ProgressFunc, usage *llmusage.Collector) (map[string]any, error)

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

func launchLocalJob(settings config.Settings, jobType string, queuedMessageKey string, queuedMessage string, runningMessageKey string, runningMessage string, run localJobFunc) jobInfo {
	// 启动一个纯 Go 本地作业，保持和 legacy bridge 相同的轮询结构。
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

	go func() {
		usage := llmusage.NewCollector(settings.DeepSeekPricing)
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
		result, err := run(progress, usage)
		finished := nowFunc().UTC()
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

func finalizeLLMUsage(settings config.Settings, jobID string, jobType string, status string, collector *llmusage.Collector, finished time.Time) llmusage.Summary {
	summary := collector.Summary()
	if len(summary.Models) == 0 {
		switch jobType {
		case "sync", "reclassify":
			summary.Models = []string{settings.ClassifierModel}
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
	// 把新作业写入内存注册表。
	apiJobs.mu.Lock()
	defer apiJobs.mu.Unlock()
	apiJobs.jobs[job.ID] = job
}

func reserveJobUnlessActive(job jobInfo) (jobInfo, bool) {
	// 同一类型的活跃作业只保留一个，避免并发请求穿过先查后写的竞态窗口。
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
		return active, true
	}
	apiJobs.jobs[job.ID] = job
	return job, false
}

func isActiveJobStatus(status string) bool {
	switch status {
	case "queued", "running", "waiting_for_user":
		return true
	default:
		return false
	}
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
