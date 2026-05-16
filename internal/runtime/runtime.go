package appruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	AppPublicName     = "FeedMeDaily"
	AppInternalName   = "scirssagent"
	SchedulerTaskName = AppPublicName + " Daily Sync"
	DefaultDailyTime  = "10:00"

	ModeSource  = "source"
	ModeRelease = "release"
)

type State struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	StartedAt string `json:"started_at,omitempty"`
}

type TraySchedulerSettings struct {
	ScheduleEnabled bool   `json:"schedule_enabled"`
	DailyTime       string `json:"daily_time"`
	LastRunDate     string `json:"last_run_date,omitempty"`
	LaunchAtLogin   bool   `json:"launch_at_login"`
}

var buildVersion = ""

func ResolveAppRoot(root string) (string, error) {
	// 优先使用显式传入的 root；否则退回当前工作目录。
	if strings.TrimSpace(root) != "" {
		return filepath.Abs(root)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(cwd)
}

func DetectMode(root string) string {
	// 允许环境变量强制指定 source/release，便于测试和打包脚本复用。
	override := strings.ToLower(strings.TrimSpace(os.Getenv("FEEDMEDAILY_RUNTIME_MODE")))
	switch override {
	case ModeRelease:
		return ModeRelease
	case ModeSource:
		return ModeSource
	}
	if LooksLikeSourceRoot(root) {
		return ModeSource
	}
	return ModeRelease
}

func LooksLikeSourceRoot(path string) bool {
	// 通过仓库根目录标志判断当前是否处于 source 模式，不要求 Python 源码目录仍然存在。
	_, pyprojectErr := os.Stat(filepath.Join(path, "pyproject.toml"))
	_, gomodErr := os.Stat(filepath.Join(path, "go.mod"))
	return pyprojectErr == nil || gomodErr == nil
}

func DefaultUserDataDir() string {
	// 发布模式下的用户数据目录优先读环境变量，其次跟随 Windows 习惯路径。
	if override := strings.TrimSpace(os.Getenv("FEEDMEDAILY_DATA_ROOT")); override != "" {
		return override
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		return filepath.Join(localAppData, AppPublicName)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", AppPublicName)
	}
	return filepath.Join(homeDir, "AppData", "Local", AppPublicName)
}

func ResolveWebDistDir(root string) string {
	// 为源码构建、发布目录和备用目录提供一组固定候选位置。
	candidates := []string{
		filepath.Join(root, "web", "dist"),
		filepath.Join(root, "dist", "web"),
		filepath.Join(root, "web_dist"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			abs, absErr := filepath.Abs(candidate)
			if absErr == nil {
				return abs
			}
			return candidate
		}
	}
	abs, err := filepath.Abs(candidates[0])
	if err != nil {
		return candidates[0]
	}
	return abs
}

func PackageVersion(root string) string {
	// 优先使用构建时注入的版本；开发态再回退到 web/package.json。
	if strings.TrimSpace(buildVersion) != "" {
		return strings.TrimSpace(buildVersion)
	}
	data, err := os.ReadFile(filepath.Join(root, "web", "package.json"))
	if err != nil {
		return "0.0.0"
	}
	match := regexp.MustCompile(`(?m)"version"\s*:\s*"([^"]+)"`).FindSubmatch(data)
	if len(match) != 2 {
		return "0.0.0"
	}
	return string(match[1])
}

func SourceBinaryPath(root string, name string) string {
	return filepath.Join(root, ".tmp", "runtime-bin", name)
}

func EnsureSourceBinary(root string, packagePath string, outputName string) (string, error) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("go command not found; install Go to build %s from source", outputName)
	}
	outputPath := SourceBinaryPath(root, outputName)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", fmt.Errorf("create runtime build directory: %w", err)
	}
	args := []string{
		"build",
		"-ldflags",
		sourceBinaryLdflags(root, outputName),
		"-o",
		outputPath,
		packagePath,
	}
	cmd := exec.Command(goPath, args...)
	cmd.Dir = root
	cmd.SysProcAttr = hiddenBuildSysProcAttr()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("build %s: %s", outputName, detail)
	}
	return outputPath, nil
}

func sourceBinaryLdflags(root string, outputName string) string {
	parts := []string{
		fmt.Sprintf("-X github.com/yyngfive/scirssagent/internal/runtime.buildVersion=%s", PackageVersion(root)),
	}
	if goruntime.GOOS == "windows" && strings.EqualFold(filepath.Ext(outputName), ".exe") {
		parts = append([]string{"-H=windowsgui"}, parts...)
	}
	return strings.Join(parts, " ")
}

func ParseVersionParts(version string) []int {
	// 把 1.2.3 或 1.2.3-beta 这类版本号转换成可比较的整数切片。
	parts := strings.Split(version, ".")
	normalized := make([]int, 0, len(parts))
	for _, part := range parts {
		digits := strings.Builder{}
		for _, char := range part {
			if char >= '0' && char <= '9' {
				digits.WriteRune(char)
			}
		}
		if digits.Len() == 0 {
			normalized = append(normalized, 0)
			continue
		}
		value, err := strconv.Atoi(digits.String())
		if err != nil {
			normalized = append(normalized, 0)
			continue
		}
		normalized = append(normalized, value)
	}
	return normalized
}

func IsNewerVersion(candidate string, current string) bool {
	// 逐段比较版本号，用于更新检查。
	left := ParseVersionParts(candidate)
	right := ParseVersionParts(current)
	limit := len(left)
	if len(right) > limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		leftValue := 0
		rightValue := 0
		if i < len(left) {
			leftValue = left[i]
		}
		if i < len(right) {
			rightValue = right[i]
		}
		if leftValue == rightValue {
			continue
		}
		return leftValue > rightValue
	}
	return false
}

func FindAvailablePort(host string, preferred int) (int, error) {
	// 先尝试偏好端口，再尝试附近端口，最后退回系统随机端口。
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

func ReadState(path string) (*State, error) {
	// 读取 daemon 的运行时状态文件；文件不存在时返回 nil 而不是报错。
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read runtime state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse runtime state: %w", err)
	}
	if state.PID <= 0 || state.Port <= 0 {
		return nil, nil
	}
	return &state, nil
}

func WriteState(path string, state State) error {
	// 用临时文件替换的方式原子写入 runtime.json，避免半写入状态。
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create runtime state dir: %w", err)
	}
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

func ClearState(path string) error {
	// 退出服务时删除 runtime.json；如果文件本来就不存在则视为成功。
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func NewState(port int) State {
	// 根据当前进程信息构造一份新的运行时状态。
	return State{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func LoadTraySchedulerSettings(path string) (TraySchedulerSettings, error) {
	// 读取托盘本地调度设置；没有文件时回退到默认值。
	settings := DefaultTraySchedulerSettings()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return settings, nil
		}
		return TraySchedulerSettings{}, fmt.Errorf("read tray scheduler settings: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return TraySchedulerSettings{}, fmt.Errorf("parse tray scheduler settings: %w", err)
	}
	if err := settings.Normalize(); err != nil {
		return TraySchedulerSettings{}, err
	}
	return settings, nil
}

func DefaultTraySchedulerSettings() TraySchedulerSettings {
	// 返回托盘调度设置的默认值。
	return TraySchedulerSettings{
		ScheduleEnabled: false,
		DailyTime:       DefaultDailyTime,
		LaunchAtLogin:   false,
	}
}

func (s *TraySchedulerSettings) Normalize() error {
	// 统一把 daily_time 修正为 HH:MM，并验证格式是否合法。
	if strings.TrimSpace(s.DailyTime) == "" {
		s.DailyTime = DefaultDailyTime
	}
	s.DailyTime = NormalizeDailyTime(s.DailyTime)
	if s.DailyTime == "" {
		return errors.New("tray scheduler daily_time must be in HH:MM format")
	}
	return nil
}

func (s TraySchedulerSettings) Save(path string) error {
	// 持久化托盘调度设置，供 API 和托盘共享。
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create tray scheduler dir: %w", err)
	}
	if err := (&s).Normalize(); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tray scheduler settings: %w", err)
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o644); err != nil {
		return fmt.Errorf("write tray scheduler temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace tray scheduler settings: %w", err)
	}
	return nil
}

func NormalizeDailyTime(value string) string {
	// 把用户输入时间标准化为 HH:MM；非法值返回空字符串。
	clean := strings.TrimSpace(value)
	parts := strings.Split(clean, ":")
	if len(parts) != 2 {
		return ""
	}
	hour, errHour := strconv.Atoi(parts[0])
	minute, errMinute := strconv.Atoi(parts[1])
	if errHour != nil || errMinute != nil {
		return ""
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", hour, minute)
}

func OpenExternalTarget(target string) error {
	// URL 交给系统默认浏览器；本地路径交给系统文件管理器或默认处理程序。
	parsed, err := url.Parse(target)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return openWithShell(target)
	}
	return openWithShell(filepath.Clean(target))
}

func openWithShell(target string) error {
	// 按平台调用系统壳层命令打开链接或路径。
	switch goruntime.GOOS {
	case "windows":
		cmd := exec.Command("cmd", "/c", "start", "", target)
		return cmd.Start()
	case "darwin":
		return exec.Command("open", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}

func ProcessRunning(pid int) bool {
	// 判断某个 PID 是否仍在运行，供托盘和 API 做状态判断。
	if pid <= 0 {
		return false
	}
	if goruntime.GOOS == "windows" {
		cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid))
		output, err := cmd.Output()
		return err == nil && strings.Contains(string(output), strconv.Itoa(pid))
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
