package api

import (
	"context"
	"errors"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

func TestBackendRunCommandLinuxSourceMode(t *testing.T) {
	settings := config.Settings{
		Mode:    appruntime.ModeSource,
		RootDir: "/tmp/feedmedaily",
	}
	command, err := backendRunCommandForPlatform(settings, "linux")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"bash", filepath.Join(settings.RootDir, "tools", "feedmedaily.sh"), "sync"}
	if len(command) != len(expected) {
		t.Fatalf("command length = %d, want %d (%#v)", len(command), len(expected), command)
	}
	for index := range expected {
		if command[index] != expected[index] {
			t.Fatalf("command[%d] = %q, want %q", index, command[index], expected[index])
		}
	}
}

func TestBackendRunCommandUnsupportedPlatforms(t *testing.T) {
	settings := config.Settings{
		Mode:    appruntime.ModeSource,
		RootDir: "/tmp/feedmedaily",
	}
	command, err := backendRunCommandForPlatform(settings, goruntime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	if goruntime.GOOS == "linux" {
		if len(command) == 0 {
			t.Fatalf("expected linux to return a helper command")
		}
		return
	}
	if len(command) != 0 {
		t.Fatalf("expected non-linux platform to omit helper command, got %#v", command)
	}
}

func TestSummarizeProfileProposalRejectedResult(t *testing.T) {
	message := summarizeResult("profile-proposal", map[string]any{
		"accepted":        false,
		"hard_rejected":   true,
		"summary":         "Removed key negative boundary.",
		"blocking_issues": []string{"Surface adjacency boundary was removed."},
		"required_fixes":  []string{"Preserve the boundary."},
	})
	if !strings.Contains(message, "Profile proposal rejected by safety review.") || !strings.Contains(message, "Removed key negative boundary.") {
		t.Fatalf("unexpected summary: %s", message)
	}
}

// 失败的任务要保留已经完成的部分结果和 warning，否则 Dashboard 只显示全 0，
// 用户看不出这条 sync 其实抓取和 enrichment 都跑了一大半。
func TestFailedJobKeepsPartialResultAndWarnings(t *testing.T) {
	restore := stubAPIGlobals(t)
	defer restore()
	settings := testSettings(t.TempDir())
	job := launchLocalJob(settings, "sync", "job.queued", "Queued.", "job.started", "Sync started.",
		func(context.Context, jobruntime.ProgressFunc, *llmusage.Collector) (map[string]any, error) {
			return map[string]any{
					"fetched":    47,
					"inserted":   0,
					"updated":    0,
					"classified": 0,
					"errors":     []string{"paper 23625: could not clear the rejected DOI: paper key is taken"},
				}, errors.New("repairing the paper DOI would collide with another paper key")
		}, nil)

	finished := waitForJobTerminalStatus(t, job.ID)
	if finished.Status != "failed" {
		t.Fatalf("expected the job to fail, got %#v", finished)
	}
	if finished.Error == "" {
		t.Fatalf("failed job lost its error message: %#v", finished)
	}
	if got := finished.Result["fetched"]; got != 47 {
		t.Fatalf("partial sync counters were dropped on failure: %#v", finished.Result)
	}
	if finished.WarningCount != 1 {
		t.Fatalf("failed job must still report its warnings: %#v", finished)
	}
	if got, ok := finished.Result["classified"]; !ok || got != 0 {
		t.Fatalf("expected the partial result map to survive: %#v", finished.Result)
	}
}
