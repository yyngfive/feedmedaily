package trayapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

var (
	shell32Runtime     = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW  = shell32Runtime.NewProc("ShellExecuteW")
	lookPath           = exec.LookPath
	ensureSourceBinary = appruntime.EnsureSourceBinary
)

type RuntimeState struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	StartedAt string `json:"started_at,omitempty"`
}

func ReadRuntimeState(path string) (*RuntimeState, error) {
	// 读取托盘视角下的 runtime.json；缺失时返回 nil。
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read runtime state: %w", err)
	}

	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse runtime state: %w", err)
	}
	if state.PID <= 0 || state.Port <= 0 {
		return nil, nil
	}
	return &state, nil
}

func WriteRuntimeState(path string, state RuntimeState) error {
	// 把当前后台服务进程信息写入 runtime.json。
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime state: %w", err)
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o644); err != nil {
		return fmt.Errorf("write runtime state temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace runtime state: %w", err)
	}
	return nil
}

func ClearRuntimeState(path string) error {
	// 删除 runtime.json；文件本来不存在时也算成功。
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func ProcessRunning(pid int) bool {
	// 用 tasklist 判断指定 PID 是否仍存在。
	if pid <= 0 {
		return false
	}
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid))
	cmd.SysProcAttr = hiddenSysProcAttr()
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), strconv.Itoa(pid))
}

func FindAvailablePort(host string, preferred int) (int, error) {
	// 优先尝试默认端口，再向后探测一小段范围。
	candidates := []int{preferred}
	for port := preferred + 1; port < preferred+20; port++ {
		candidates = append(candidates, port)
	}
	for _, port := range candidates {
		listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			_ = listener.Close()
			return port, nil
		}
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("unexpected listener address type")
	}
	return addr.Port, nil
}

func WaitForHealthcheck(url string, timeout time.Duration) bool {
	// 轮询 /api/app/health，直到服务可用或超时。
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					return true
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func OpenBrowser(url string) error {
	// 调用 Windows Shell 打开默认浏览器。
	return shellExecute("open", url)
}

func OpenPath(path string) error {
	// 调用 Windows Shell 打开目录或文件。
	return shellExecute("open", filepath.Clean(path))
}

func hiddenSysProcAttr() *syscall.SysProcAttr {
	// 创建后台无窗口子进程时统一复用的启动属性。
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
}

func shellOpenSysProcAttr() *syscall.SysProcAttr {
	// 为 shell 打开动作保留的轻量属性，目前主要用于兼容扩展。
	return &syscall.SysProcAttr{HideWindow: true}
}

func shellExecute(verb string, target string) error {
	// 直接调用 ShellExecuteW 打开链接、文件或目录。
	verbPtr, err := syscall.UTF16PtrFromString(verb)
	if err != nil {
		return err
	}
	targetPtr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	result, _, callErr := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(targetPtr)),
		0,
		0,
		1,
	)
	if result <= 32 {
		return fmt.Errorf("ShellExecuteW failed for %s: %v", target, callErr)
	}
	return nil
}

func launchDetached(command []string, cwd string) (int, error) {
	// 以完全后台化的方式启动 Go 后端，不弹出终端窗口。
	if len(command) == 0 {
		return 0, errors.New("backend command is empty")
	}
	stdinFile, err := os.Open(os.DevNull)
	if err != nil {
		return 0, err
	}
	defer stdinFile.Close()

	stdoutFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}
	defer stdoutFile.Close()

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = cwd
	cmd.Stdout = stdoutFile
	cmd.Stderr = stdoutFile
	cmd.Stdin = stdinFile
	cmd.Env = append(os.Environ(), "FEEDMEDAILY_LAUNCHED_BY_TRAY=1")
	cmd.SysProcAttr = hiddenSysProcAttr()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

func httpPostJSON(url string, payload any) error {
	// 发送一个简单的 JSON POST 请求，用于调用本地控制接口。
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}
	return nil
}

func backendCommand(layout Layout, port int) ([]string, error) {
	// 根据当前模式组装启动 feedmedailyd 的命令行。
	if layout.Mode == runtimeModeRelease {
		goServiceCandidates := []string{
			filepath.Join(layout.RootDir, "feedmedailyd.exe"),
			filepath.Join(layout.RootDir, "FeedMeDailyD.exe"),
		}
		for _, executable := range goServiceCandidates {
			if _, err := os.Stat(executable); err == nil {
				return []string{
					executable,
					"--root",
					layout.RootDir,
					"--host",
					layout.ServerHost,
					"--port",
					strconv.Itoa(port),
				}, nil
			}
		}
		return nil, errors.New("go backend executable not found: expected feedmedailyd.exe in the app directory")
	}

	if _, err := lookPath("go"); err == nil {
		binaryPath, err := ensureSourceBinary(layout.RootDir, "./cmd/feedmedailyd", "feedmedailyd.exe")
		if err != nil {
			return nil, err
		}
		return []string{
			binaryPath,
			"--root",
			layout.RootDir,
			"--host",
			layout.ServerHost,
			"--port",
			strconv.Itoa(port),
		}, nil
	}

	return nil, errors.New("go command not found; install Go to run feedmedailyd from source")
}

func ensureService(layout Layout) (string, error) {
	// 复用现有服务；若服务不存在或失活，则拉起新的后台服务。
	state, err := ReadRuntimeState(layout.RuntimeStatePath)
	if err != nil {
		return "", err
	}
	if state != nil {
		baseURL := fmt.Sprintf("http://%s:%d", layout.ServerHost, state.Port)
		if WaitForHealthcheck(baseURL+"/api/app/health", 1200*time.Millisecond) {
			return baseURL, nil
		}
		if !ProcessRunning(state.PID) {
			_ = ClearRuntimeState(layout.RuntimeStatePath)
			state = nil
		}
	}

	port, err := FindAvailablePort(layout.ServerHost, layout.ServerPort)
	if err != nil {
		return "", fmt.Errorf("find available port: %w", err)
	}

	command, err := backendCommand(layout, port)
	if err != nil {
		return "", err
	}
	pid, err := launchDetached(command, layout.RootDir)
	if err != nil {
		return "", fmt.Errorf("start backend: %w", err)
	}

	baseURL := fmt.Sprintf("http://%s:%d", layout.ServerHost, port)
	if !WaitForHealthcheck(baseURL+"/api/app/health", 12*time.Second) {
		return "", errors.New("backend service did not become healthy in time")
	}

	if err := WriteRuntimeState(layout.RuntimeStatePath, RuntimeState{
		PID:       pid,
		Port:      port,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return "", err
	}

	return baseURL, nil
}

func stopService(layout Layout) error {
	// 优先请求 API 正常退出；失败时再强制 taskkill。
	state, err := ReadRuntimeState(layout.RuntimeStatePath)
	if err != nil {
		return err
	}
	if state == nil {
		return nil
	}

	baseURL := fmt.Sprintf("http://%s:%d", layout.ServerHost, state.Port)
	if WaitForHealthcheck(baseURL+"/api/app/health", 1200*time.Millisecond) {
		_ = httpPostJSON(baseURL+"/api/app/exit", nil)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !ProcessRunning(state.PID) {
				return ClearRuntimeState(layout.RuntimeStatePath)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}

	if ProcessRunning(state.PID) {
		cmd := exec.Command("taskkill", "/PID", strconv.Itoa(state.PID), "/T", "/F")
		cmd.SysProcAttr = hiddenSysProcAttr()
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("taskkill failed: %w", err)
		}
	}
	return ClearRuntimeState(layout.RuntimeStatePath)
}

func triggerSync(baseURL string) error {
	// 通过 Go backend 的 admin/run 接口触发一次同步任务。
	return httpPostJSON(baseURL+"/api/admin/run", nil)
}
