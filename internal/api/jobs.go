package api

import (
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/logging"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

type jobInfo struct {
	ID                   string         `json:"id"`
	JobType              string         `json:"job_type"`
	Status               string         `json:"status"`
	MessageKey           string         `json:"message_key,omitempty"`
	Message              string         `json:"message,omitempty"`
	Error                string         `json:"error,omitempty"`
	VerificationRequired bool           `json:"verification_required,omitempty"`
	VerificationTarget   string         `json:"verification_target,omitempty"`
	VerificationFeedURL  string         `json:"verification_feed_url,omitempty"`
	VerificationJournal  string         `json:"verification_journal,omitempty"`
	Result               map[string]any `json:"result,omitempty"`
	LogPath              string         `json:"log_path,omitempty"`
	WarningCount         int            `json:"warning_count,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	StartedAt            *time.Time     `json:"started_at,omitempty"`
	FinishedAt           *time.Time     `json:"finished_at,omitempty"`
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

type localJobFunc func(progress func(string, string)) (map[string]any, error)

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

func launchLocalJob(logsDir string, jobType string, queuedMessageKey string, queuedMessage string, runningMessageKey string, runningMessage string, run localJobFunc) jobInfo {
	// 启动一个纯 Go 本地作业，保持和 legacy bridge 相同的轮询结构。
	job := jobInfo{
		ID:         nextJobID(),
		JobType:    jobType,
		Status:     "queued",
		MessageKey: queuedMessageKey,
		Message:    queuedMessage,
		CreatedAt:  nowFunc().UTC(),
	}
	if path, err := logging.Write(logsDir, logging.Event{
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
		started := nowFunc().UTC()
		logJobEvent(logsDir, &job, "info", "started", runningMessageKey, runningMessage, "", nil)
		updateJob(job.ID, func(current *jobInfo) {
			current.Status = "running"
			current.MessageKey = runningMessageKey
			current.Message = runningMessage
			current.StartedAt = &started
		})

		progress := func(messageKey string, message string) {
			logJobEvent(logsDir, &job, "info", "progress", messageKey, message, "", nil)
			updateJob(job.ID, func(current *jobInfo) {
				current.MessageKey = messageKey
				current.Message = message
			})
		}
		result, err := run(progress)
		finished := nowFunc().UTC()
		if err != nil {
			logJobEvent(logsDir, &job, "error", "failed", jobType+".failed", "", err.Error(), nil)
			updateJob(job.ID, func(current *jobInfo) {
				current.Status = "failed"
				current.MessageKey = jobType + ".failed"
				current.Message = ""
				current.Error = err.Error()
				current.FinishedAt = &finished
			})
			return
		}

		warningCount := countWarnings(result)
		logJobEvent(logsDir, &job, "info", "completed", jobType+".completed", "Completed.", "", result)
		updateJob(job.ID, func(current *jobInfo) {
			current.Status = "completed"
			current.MessageKey = jobType + ".completed"
			current.Message = summarizeResult(jobType, result)
			current.Result = result
			current.WarningCount = warningCount
			current.FinishedAt = &finished
		})
	}()

	return job
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
