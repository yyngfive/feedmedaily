package api

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

type jobInfo struct {
	ID         string         `json:"id"`
	JobType    string         `json:"job_type"`
	Status     string         `json:"status"`
	MessageKey string         `json:"message_key,omitempty"`
	Message    string         `json:"message,omitempty"`
	Error      string         `json:"error,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
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

func launchLocalJob(jobType string, queuedMessageKey string, queuedMessage string, runningMessageKey string, runningMessage string, run localJobFunc) jobInfo {
	// 启动一个纯 Go 本地作业，保持和 legacy bridge 相同的轮询结构。
	job := jobInfo{
		ID:         nextJobID(),
		JobType:    jobType,
		Status:     "queued",
		MessageKey: queuedMessageKey,
		Message:    queuedMessage,
		CreatedAt:  nowFunc().UTC(),
	}
	storeJob(job)

	go func() {
		started := nowFunc().UTC()
		updateJob(job.ID, func(current *jobInfo) {
			current.Status = "running"
			current.MessageKey = runningMessageKey
			current.Message = runningMessage
			current.StartedAt = &started
		})

		progress := func(messageKey string, message string) {
			updateJob(job.ID, func(current *jobInfo) {
				current.MessageKey = messageKey
				current.Message = message
			})
		}
		result, err := run(progress)
		finished := nowFunc().UTC()
		if err != nil {
			updateJob(job.ID, func(current *jobInfo) {
				current.Status = "failed"
				current.MessageKey = jobType + ".failed"
				current.Message = ""
				current.Error = err.Error()
				current.FinishedAt = &finished
			})
			return
		}

		updateJob(job.ID, func(current *jobInfo) {
			current.Status = "completed"
			current.MessageKey = jobType + ".completed"
			current.Message = "Completed."
			current.Result = result
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
	// 返回当前正式生产后端的一次同步命令行，供调度说明和外部壳层复用。
	return backendCommand(settings, []string{"--run-once"})
}

func backendCommand(settings config.Settings, args []string) ([]string, error) {
	// 根据 source/release 模式解析出正式 Go 后端入口。
	if settings.Mode == appruntime.ModeRelease {
		return append([]string{filepath.Join(settings.AppDir, "feedmedailyd.exe")}, append(args, "--root", settings.RootDir)...), nil
	}

	if goPath, err := exec.LookPath("go"); err == nil {
		command := []string{goPath, "run", "./cmd/feedmedailyd"}
		command = append(command, args...)
		command = append(command, "--root", settings.RootDir)
		return command, nil
	}

	return nil, fmt.Errorf("go command not found; install Go to run feedmedailyd from source")
}
