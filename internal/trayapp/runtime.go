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
)

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

var (
	shell32Runtime    = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW = shell32Runtime.NewProc("ShellExecuteW")
)

type RuntimeState struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	StartedAt string `json:"started_at,omitempty"`
}

func ReadRuntimeState(path string) (*RuntimeState, error) {
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
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func ProcessRunning(pid int) bool {
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
	return shellExecute("open", url)
}

func OpenPath(path string) error {
	return shellExecute("open", filepath.Clean(path))
}

func hiddenSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
}

func shellOpenSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

func shellExecute(verb string, target string) error {
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
	cmd.SysProcAttr = hiddenSysProcAttr()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

func httpPostJSON(url string, payload any) error {
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

func backendCommand(layout Layout, settings TraySettings, port int) ([]string, error) {
	if len(settings.BackendCommand) > 0 {
		return expandCommandTemplate(settings.BackendCommand, layout, port), nil
	}

	if layout.Mode == runtimeModeRelease {
		executable := filepath.Join(layout.RootDir, "FeedMeDaily.exe")
		if _, err := os.Stat(executable); err == nil {
			return []string{
				executable,
				"serve",
				"--host",
				layout.ServerHost,
				"--port",
				strconv.Itoa(port),
			}, nil
		}
	}

	venvPythonw := filepath.Join(layout.RootDir, ".venv", "Scripts", "pythonw.exe")
	if _, err := os.Stat(venvPythonw); err == nil {
		return []string{
			venvPythonw,
			"-m",
			"scirssagent.cli",
			"serve",
			"--root",
			layout.RootDir,
			"--host",
			layout.ServerHost,
			"--port",
			strconv.Itoa(port),
		}, nil
	}

	venvPython := filepath.Join(layout.RootDir, ".venv", "Scripts", "python.exe")
	if _, err := os.Stat(venvPython); err == nil {
		return []string{
			venvPython,
			"-m",
			"scirssagent.cli",
			"serve",
			"--root",
			layout.RootDir,
			"--host",
			layout.ServerHost,
			"--port",
			strconv.Itoa(port),
		}, nil
	}

	if pythonwPath, err := exec.LookPath("pythonw"); err == nil {
		return []string{
			pythonwPath,
			"-m",
			"scirssagent.cli",
			"serve",
			"--root",
			layout.RootDir,
			"--host",
			layout.ServerHost,
			"--port",
			strconv.Itoa(port),
		}, nil
	}

	if pythonPath, err := exec.LookPath("python"); err == nil {
		return []string{
			pythonPath,
			"-m",
			"scirssagent.cli",
			"serve",
			"--root",
			layout.RootDir,
			"--host",
			layout.ServerHost,
			"--port",
			strconv.Itoa(port),
		}, nil
	}

	if uvPath, err := exec.LookPath("uv"); err == nil {
		return []string{
			uvPath,
			"run",
			"--project",
			layout.RootDir,
			"scirssagent",
			"serve",
			"--root",
			layout.RootDir,
			"--host",
			layout.ServerHost,
			"--port",
			strconv.Itoa(port),
		}, nil
	}

	return nil, errors.New("no backend command found; configure tray-settings.json backend_command or install Python/uv")
}

func expandCommandTemplate(command []string, layout Layout, port int) []string {
	replacements := map[string]string{
		"{root}": layout.RootDir,
		"{host}": layout.ServerHost,
		"{port}": strconv.Itoa(port),
	}
	expanded := make([]string, 0, len(command))
	for _, item := range command {
		value := item
		for needle, replacement := range replacements {
			value = strings.ReplaceAll(value, needle, replacement)
		}
		expanded = append(expanded, value)
	}
	return expanded
}

func ensureService(layout Layout, settings TraySettings) (string, error) {
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

	command, err := backendCommand(layout, settings, port)
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
	return httpPostJSON(baseURL+"/api/admin/run", nil)
}
