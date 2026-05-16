package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/yyngfive/scirssagent/internal/api"
	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/logging"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
	zoterosvc "github.com/yyngfive/scirssagent/internal/zotero"
)

func main() {
	// 解析启动参数，允许托盘或命令行覆盖根目录、监听地址和端口。
	root := flag.String("root", "", "Project root or installed app directory.")
	host := flag.String("host", "", "Server host. Defaults to configured value.")
	port := flag.Int("port", 0, "Server port. Defaults to configured value.")
	runOnce := flag.Bool("run-once", false, "Run one fetch/classify/report cycle and exit.")
	maxPapers := flag.Int("max-papers", 0, "Limit the number of fetched papers for a test run.")
	reclassify := flag.Bool("reclassify", false, "Force touched papers through classification again.")
	reportLatest := flag.Bool("report-latest", false, "Rebuild and publish the latest report, then exit.")
	zoteroCollections := flag.Bool("zotero-collections", false, "List Zotero collections and exit.")
	zoteroSave := flag.Bool("zotero-save", false, "Save one paper to Zotero and exit.")
	paperID := flag.Int64("paper-id", 0, "Paper id used together with --zotero-save.")
	collectionKey := flag.String("collection-key", "", "Optional Zotero collection key used together with --zotero-save.")
	flag.Parse()

	// 统一加载运行布局和本地设置。
	settings, err := config.Load(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logging.SetDefaultDir(settings.LogsDir)
	if *runOnce {
		logCommandEvent(settings.LogsDir, "info", "cli", "run_once_started", "Starting one fetch/classify/report cycle.", map[string]any{
			"max_papers": *maxPapers,
			"reclassify": *reclassify,
			"command":    "run-once",
		})
		summary, err := jobruntime.RunOnce(settings, jobruntime.RunOptions{
			MaxPapers:  *maxPapers,
			Reclassify: *reclassify,
		}, progressReporter(settings.LogsDir, "cli.run_once"))
		if err != nil {
			logCommandEvent(settings.LogsDir, "error", "cli", "run_once_failed", "", map[string]any{"error": err.Error()})
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		logCommandEvent(settings.LogsDir, "info", "cli", "run_once_completed", "Run completed.", map[string]any{
			"fetched":    summary.Fetched,
			"inserted":   summary.Inserted,
			"updated":    summary.Updated,
			"classified": summary.Classified,
			"errors":     summary.Errors,
		})
		if err := json.NewEncoder(os.Stdout).Encode(summary); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *reportLatest {
		logCommandEvent(settings.LogsDir, "info", "cli", "report_latest_started", "Rebuilding the latest report payload.", nil)
		reportCount, err := jobruntime.RebuildLatestReport(settings, progressReporter(settings.LogsDir, "cli.report_latest"))
		if err != nil {
			logCommandEvent(settings.LogsDir, "error", "cli", "report_latest_failed", "", map[string]any{"error": err.Error()})
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		logCommandEvent(settings.LogsDir, "info", "cli", "report_latest_completed", "Report rebuild completed.", map[string]any{"report_papers": reportCount})
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"report_papers": reportCount}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *zoteroCollections {
		logCommandEvent(settings.LogsDir, "info", "cli", "zotero_collections_started", "Listing Zotero collections.", nil)
		payload, err := zoterosvc.ListCollections(settings)
		if err != nil {
			logCommandEvent(settings.LogsDir, "error", "cli", "zotero_collections_failed", "", map[string]any{"error": err.Error()})
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		logCommandEvent(settings.LogsDir, "info", "cli", "zotero_collections_completed", "Zotero collections listed.", map[string]any{"collections": len(payload.Collections)})
		if err := json.NewEncoder(os.Stdout).Encode(payload); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *zoteroSave {
		if *paperID < 1 {
			fmt.Fprintln(os.Stderr, "--paper-id is required with --zotero-save")
			os.Exit(1)
		}
		logCommandEvent(settings.LogsDir, "info", "cli", "zotero_save_started", "Saving one paper to Zotero.", map[string]any{"paper_id": *paperID})
		sqliteStore, err := store.Open(settings.DatabasePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer sqliteStore.Close()
		paper, err := sqliteStore.PaperByID(*paperID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if paper == nil {
			fmt.Fprintln(os.Stderr, "Paper not found.")
			os.Exit(1)
		}
		classification, err := sqliteStore.LatestClassification(*paperID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if classification == nil {
			fmt.Fprintln(os.Stderr, "Paper has no classification yet.")
			os.Exit(1)
		}
		current, err := sqliteStore.LatestZoteroStatus(*paperID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if current != nil && current.Saved {
			if err := json.NewEncoder(os.Stdout).Encode(current); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		var selectedCollectionKey *string
		if *collectionKey != "" {
			selectedCollectionKey = collectionKey
		}
		itemKey, saveErr := zoterosvc.SavePaper(settings, *paper, *classification, selectedCollectionKey)
		if saveErr != nil {
			logCommandEvent(settings.LogsDir, "error", "cli", "zotero_save_failed", "", map[string]any{"paper_id": *paperID, "error": saveErr.Error()})
			status, err := sqliteStore.UpsertZoteroStatus(*paperID, "error", nil, stringPtr(saveErr.Error()), time.Now().UTC())
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if err := json.NewEncoder(os.Stdout).Encode(status); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		status, err := sqliteStore.UpsertZoteroStatus(*paperID, "saved", itemKey, nil, time.Now().UTC())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		logCommandEvent(settings.LogsDir, "info", "cli", "zotero_save_completed", "Paper saved to Zotero.", map[string]any{"paper_id": *paperID, "item_key": itemKey})
		if err := json.NewEncoder(os.Stdout).Encode(status); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if settings.Mode == appruntime.ModeRelease && os.Getenv("FEEDMEDAILY_LAUNCHED_BY_TRAY") != "1" {
		if err := appruntime.EnsureTrayRunning(settings.RootDir); err != nil {
			fmt.Fprintln(os.Stderr, "warning: ensure tray running:", err)
		}
	}
	// 显式传参优先于配置文件中的默认值。
	if *host != "" {
		settings.ServerHost = *host
	}
	if *port > 0 {
		settings.ServerPort = *port
	}

	// 先占住端口，再把 listener 交给 HTTP 服务器，避免竞态。
	address := net.JoinHostPort(settings.ServerHost, strconv.Itoa(settings.ServerPort))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}

	// 创建 API 服务器，并把“退出”动作接到优雅关闭逻辑上。
	server := &http.Server{Addr: address}
	apiServer := api.NewServer(settings, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	server.Handler = apiServer.Handler()

	// 把当前 daemon 的 PID 和端口写到 runtime.json，供托盘探活和停止服务使用。
	if err := appruntime.WriteState(settings.RuntimeStatePath, appruntime.NewState(settings.ServerPort)); err != nil {
		fmt.Fprintln(os.Stderr, "write runtime state:", err)
		os.Exit(1)
	}
	defer appruntime.ClearState(settings.RuntimeStatePath)

	// 监听 Ctrl+C 或系统终止信号，确保命令行运行时也能正常退出。
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

func stringPtr(value string) *string {
	clean := value
	return &clean
}

func progressReporter(logsDir string, component string) jobruntime.ProgressFunc {
	return func(messageKey string, message string) {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", time.Now().Format("15:04:05"), message)
		_, _ = logging.Write(logsDir, logging.Event{
			Level:      "info",
			Component:  component,
			Action:     "progress",
			MessageKey: messageKey,
			Message:    message,
		})
	}
}

func logCommandEvent(logsDir string, level string, component string, action string, message string, data map[string]any) {
	eventData := data
	if eventData == nil {
		eventData = map[string]any{}
	}
	if rawError, ok := eventData["error"].(string); ok && rawError != "" {
		_, _ = logging.Write(logsDir, logging.Event{
			Level:     level,
			Component: component,
			Action:    action,
			Message:   message,
			Error:     rawError,
			Data:      eventData,
		})
		return
	}
	_, _ = logging.Write(logsDir, logging.Event{
		Level:     level,
		Component: component,
		Action:    action,
		Message:   message,
		Data:      eventData,
	})
}
