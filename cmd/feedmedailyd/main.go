package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	goruntime "runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/yyngfive/scirssagent/internal/api"
	"github.com/yyngfive/scirssagent/internal/config"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

func main() {
	// 解析启动参数，只保留 daemon/server 启动所需参数。
	root := flag.String("root", "", "Project root or installed app directory.")
	host := flag.String("host", "", "Server host. Defaults to configured value.")
	port := flag.Int("port", 0, "Server port. Defaults to configured value.")
	flag.Parse()

	// 统一加载运行布局和本地设置。
	settings, err := config.Load(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
	defer apiServer.Close()
	server.Handler = apiServer.Handler()

	// 把当前 daemon 的 PID 和端口写到 runtime.json，供托盘探活和停止服务使用。
	if err := appruntime.WriteState(settings.RuntimeStatePath, appruntime.NewState(settings.ServerPort)); err != nil {
		fmt.Fprintln(os.Stderr, "write runtime state:", err)
		os.Exit(1)
	}
	defer appruntime.ClearState(settings.RuntimeStatePath)

	maybeEnsureTray(settings)
	printDaemonStartup(settings)

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
	fmt.Fprintf(os.Stderr, "[%s] FeedMeDaily server stopped.\n", time.Now().Format("15:04:05"))
}

func maybeEnsureTray(settings config.Settings) {
	if os.Getenv("FEEDMEDAILY_LAUNCHED_BY_TRAY") == "1" {
		return
	}
	if goruntime.GOOS != "windows" {
		fmt.Fprintf(
			os.Stderr,
			"[%s] Tray auto-launch is skipped on %s.\n",
			time.Now().Format("15:04:05"),
			goruntime.GOOS,
		)
		return
	}
	if err := appruntime.EnsureTrayRunning(settings.RootDir); err != nil {
		fmt.Fprintln(os.Stderr, "warning: ensure tray running:", err)
		return
	}
	fmt.Fprintf(os.Stderr, "[%s] FeedMeDaily tray is running.\n", time.Now().Format("15:04:05"))
}

func printDaemonStartup(settings config.Settings) {
	fmt.Fprintf(os.Stderr, "[%s] FeedMeDaily server started.\n", time.Now().Format("15:04:05"))
	fmt.Fprintf(os.Stderr, "  mode: %s\n", settings.Mode)
	fmt.Fprintf(os.Stderr, "  url: http://%s:%d\n", settings.ServerHost, settings.ServerPort)
	fmt.Fprintf(os.Stderr, "  logs: %s\n", settings.LogsDir)
	fmt.Fprintf(os.Stderr, "  data: %s\n", settings.DataDir)
	fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop.\n")
}
