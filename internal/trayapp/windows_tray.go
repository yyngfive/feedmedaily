//go:build windows

package trayapp

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/yyngfive/scirssagent/internal/logging"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

const (
	cwUseDefault       = 0x80000000
	idcArrow           = 32512
	idiApplication     = 32512
	imageIcon          = 1
	lrDefaultSize      = 0x00000040
	lrLoadFromFile     = 0x00000010
	mfByCommand        = 0x00000000
	mfChecked          = 0x00000008
	mfDisabled         = 0x00000002
	mfEnabled          = 0x00000000
	mfGray             = 0x00000001
	mfSeparator        = 0x00000800
	mfString           = 0x00000000
	nifIcon            = 0x00000002
	nifInfo            = 0x00000010
	nifMessage         = 0x00000001
	nifTip             = 0x00000004
	nimAdd             = 0x00000000
	nimDelete          = 0x00000002
	nimModify          = 0x00000001
	nimSetVersion      = 0x00000004
	niifError          = 0x00000003
	niifInfo           = 0x00000001
	notifyIconVersion  = 0x00000003
	swHide             = 0
	swShow             = 5
	tpmBottomAlign     = 0x0020
	tpmLeftAlign       = 0x0000
	tpmRightButton     = 0x0002
	wmApp              = 0x8000
	wmClose            = 0x0010
	wmCommand          = 0x0111
	wmContextMenu      = 0x007B
	wmDestroy          = 0x0002
	wmLButtonDblClk    = 0x0203
	wmNull             = 0x0000
	wmPowerBroadcast   = 0x0218
	wmRButtonUp        = 0x0205
	wmUser             = 0x0400
	wsOverlappedWindow = 0x00CF0000
)

const (
	menuOpenApp uint16 = 1001 + iota
	menuRunSync
	menuToggleSchedule
	menuLaunchAtLogin
	menuOpenTraySettings
	menuOpenDataDir
	menuOpenLogsDir
	menuQuit
)

const (
	pbtApmResumeAutomatic = 0x0012
	pbtApmResumeSuspend   = 0x0007
)

const (
	trayCallbackMessage  = wmApp + 1
	trayMsgRefreshIcon   = wmApp + 2
	trayMsgShowInfo      = wmApp + 3
	trayMsgShowError     = wmApp + 4
	trayMsgReloadSetting = wmApp + 5
)

const refreshRetryDelay = time.Second

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	shell32               = syscall.NewLazyDLL("shell32.dll")
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procAppendMenuW       = user32.NewProc("AppendMenuW")
	procCreateMutexW      = kernel32.NewProc("CreateMutexW")
	procCloseHandle       = kernel32.NewProc("CloseHandle")
	procCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	procCreateWindowExW   = user32.NewProc("CreateWindowExW")
	procDefWindowProcW    = user32.NewProc("DefWindowProcW")
	procDestroyMenu       = user32.NewProc("DestroyMenu")
	procDestroyWindow     = user32.NewProc("DestroyWindow")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procFindWindowW       = user32.NewProc("FindWindowW")
	procGetCursorPos      = user32.NewProc("GetCursorPos")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
	procGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
	procLoadCursorW       = user32.NewProc("LoadCursorW")
	procLoadIconW         = user32.NewProc("LoadIconW")
	procLoadImageW        = user32.NewProc("LoadImageW")
	procMessageBoxW       = user32.NewProc("MessageBoxW")
	procPostQuitMessage   = user32.NewProc("PostQuitMessage")
	procPostMessageW      = user32.NewProc("PostMessageW")
	procRegisterClassExW  = user32.NewProc("RegisterClassExW")
	procRegisterWindowMsg = user32.NewProc("RegisterWindowMessageW")
	procSetForegroundWnd  = user32.NewProc("SetForegroundWindow")
	procShellNotifyIconW  = shell32.NewProc("Shell_NotifyIconW")
	procTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procUpdateWindow      = user32.NewProc("UpdateWindow")
)

var (
	globalTray            *windowsTray
	taskbarCreatedMessage uint32
	shellNotifyIconCall   = func(message uint32, data *notifyIconData) (bool, error) {
		ok, _, err := procShellNotifyIconW.Call(uintptr(message), uintptr(unsafe.Pointer(data)))
		return ok != 0, err
	}
	findTrayWindowCall = func() uintptr {
		className, _ := syscall.UTF16PtrFromString("FeedMeDailyTrayWindow")
		title, _ := syscall.UTF16PtrFromString("FeedMeDaily Tray")
		hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)))
		return hwnd
	}
	postMessageCall = func(hwnd uintptr, message uint32, wParam uintptr, lParam uintptr) bool {
		ok, _, _ := procPostMessageW.Call(hwnd, uintptr(message), wParam, lParam)
		return ok != 0
	}
	scheduleAfterFunc = time.AfterFunc
)

type trayMenuState struct {
	ScheduleEnabled bool
	DailyTime       string
	LaunchAtLogin   bool
}

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSmall  uintptr
}

type notifyIconData struct {
	Size           uint32
	HWnd           uintptr
	ID             uint32
	Flags          uint32
	CallbackMsg    uint32
	Icon           uintptr
	Tip            [128]uint16
	State          uint32
	StateMask      uint32
	Info           [256]uint16
	VersionOrTimer uint32
	InfoTitle      [64]uint16
	InfoFlags      uint32
	GuidItem       [16]byte
	BalloonIcon    uintptr
}

type trayBalloonRequest struct {
	title     string
	body      string
	infoFlags uint32
}

type windowsTray struct {
	app                   *App
	hwnd                  uintptr
	icon                  uintptr
	mutexHandle           uintptr
	nid                   notifyIconData
	balloonMutex          sync.Mutex
	balloonQueue          []trayBalloonRequest
	refreshRetryScheduled bool
}

func newWindowsTray(app *App) (*windowsTray, error) {
	// 创建托盘实例，并通过命名互斥锁保证单实例运行。
	mutexName, err := syscall.UTF16PtrFromString(appruntime.TrayMutexName)
	if err != nil {
		return nil, err
	}
	mutexHandle, _, createErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	if mutexHandle == 0 {
		return nil, createErr
	}
	if errno, ok := createErr.(syscall.Errno); ok && errno == syscall.Errno(183) {
		return nil, fmt.Errorf("FeedMeDaily tray is already running")
	}

	return &windowsTray{
		app:         app,
		mutexHandle: mutexHandle,
	}, nil
}

func (t *windowsTray) Run() error {
	// 注册隐藏窗口、托盘图标和消息循环，进入真正的 Windows 托盘生命周期。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		globalTray = nil
		if t.mutexHandle != 0 {
			procCloseHandle.Call(t.mutexHandle)
			t.mutexHandle = 0
		}
	}()
	globalTray = t
	taskbarCreatedMessage = registerWindowMessage("TaskbarCreated")
	className, _ := syscall.UTF16PtrFromString("FeedMeDailyTrayWindow")
	instance, _, _ := procGetModuleHandleW.Call(0)
	icon, err := t.loadIcon()
	if err != nil {
		return err
	}
	t.icon = icon

	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	windowClass := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   syscall.NewCallback(windowProc),
		Instance:  instance,
		Icon:      icon,
		Cursor:    cursor,
		ClassName: className,
		IconSmall: icon,
	}

	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&windowClass))); atom == 0 {
		return fmt.Errorf("register window class: %v", err)
	}

	title, _ := syscall.UTF16PtrFromString("FeedMeDaily Tray")
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow,
		cwUseDefault,
		cwUseDefault,
		cwUseDefault,
		cwUseDefault,
		0,
		0,
		instance,
		0,
	)
	if hwnd == 0 {
		return fmt.Errorf("create hidden tray window: %v", err)
	}
	t.hwnd = hwnd

	if err := t.addTrayIcon(); err != nil {
		return err
	}

	procUpdateWindow.Call(hwnd)

	var message msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}

	return nil
}

func (t *windowsTray) addTrayIcon() error {
	// 把图标注册到系统托盘区。
	t.nid = notifyIconData{
		Size:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:           t.hwnd,
		ID:             1,
		Flags:          nifMessage | nifIcon | nifTip,
		CallbackMsg:    trayCallbackMessage,
		Icon:           t.icon,
		VersionOrTimer: notifyIconVersion,
	}
	copyUTF16ToFixedArray(t.nid.Tip[:], "FeedMeDaily Tray")
	if ok, err := shellNotifyIconCall(nimAdd, &t.nid); !ok {
		return fmt.Errorf("add tray icon: %v", err)
	}
	if ok, err := shellNotifyIconCall(nimSetVersion, &t.nid); !ok {
		return fmt.Errorf("set tray icon version: %v", err)
	}
	return nil
}

func (t *windowsTray) refreshTrayIcon() error {
	// 在任务栏重建或系统恢复后，重新注册托盘图标和回调消息。
	if t.hwnd == 0 {
		return nil
	}
	_, _ = shellNotifyIconCall(nimDelete, &t.nid)
	return t.addTrayIcon()
}

func (t *windowsTray) removeTrayIcon() {
	// 退出时从系统托盘移除图标。
	if t.hwnd != 0 {
		_, _ = shellNotifyIconCall(nimDelete, &t.nid)
	}
}

func (t *windowsTray) ShowInfo(title string, body string) {
	// 弹出普通信息气泡提示。
	t.enqueueBalloon(trayMsgShowInfo, title, body, niifInfo)
}

func (t *windowsTray) ShowError(title string, body string) {
	// 弹出错误气泡提示。
	t.enqueueBalloon(trayMsgShowError, title, body, niifError)
}

func (t *windowsTray) enqueueBalloon(message uint32, title string, body string, infoFlags uint32) {
	// 后台 goroutine 只排队并通知 UI 线程，不直接碰托盘状态。
	t.balloonMutex.Lock()
	t.balloonQueue = append(t.balloonQueue, trayBalloonRequest{
		title:     title,
		body:      body,
		infoFlags: infoFlags,
	})
	t.balloonMutex.Unlock()
	if !t.postTrayMessage(message, 0, 0) {
		t.log("error", "tray_balloon_post_failed", "Posting tray balloon message failed.", nil, nil)
	}
}

func (t *windowsTray) showBalloon(request trayBalloonRequest) {
	// 用托盘气泡向用户反馈后台操作结果。
	t.nid.Flags = nifInfo
	copyUTF16ToFixedArray(t.nid.InfoTitle[:], request.title)
	copyUTF16ToFixedArray(t.nid.Info[:], request.body)
	t.nid.InfoFlags = request.infoFlags
	if ok, err := shellNotifyIconCall(nimModify, &t.nid); !ok {
		t.log("error", "tray_balloon_failed", "Showing tray balloon failed.", map[string]any{
			"title": request.title,
		}, err)
	}
	t.nid.Flags = nifMessage | nifIcon | nifTip
}

func (t *windowsTray) handleCommand(commandID uint16) {
	// 把菜单点击映射到 App 层动作。
	switch commandID {
	case menuOpenApp:
		t.runAction(func() error { return t.app.OpenApp() }, "Opened FeedMeDaily.", "Open FeedMeDaily failed")
	case menuRunSync:
		t.runAction(func() error { return t.app.RunSyncNow() }, "Sync job started.", "Run sync failed")
	case menuToggleSchedule:
		state, err := t.app.ToggleScheduleEnabled()
		if err != nil {
			t.ShowError("FeedMeDaily Tray", "Update schedule failed: "+err.Error())
			return
		}
		if state.ScheduleEnabled {
			t.ShowInfo("FeedMeDaily Tray", fmt.Sprintf("Daily sync enabled at %s.", state.DailyTime))
		} else {
			t.ShowInfo("FeedMeDaily Tray", "Daily sync disabled.")
		}
	case menuLaunchAtLogin:
		state, err := t.app.ToggleLaunchAtLogin()
		if err != nil {
			t.ShowError("FeedMeDaily Tray", "Update launch at login failed: "+err.Error())
			return
		}
		if state.LaunchAtLogin {
			t.ShowInfo("FeedMeDaily Tray", "Launch at login enabled.")
		} else {
			t.ShowInfo("FeedMeDaily Tray", "Launch at login disabled.")
		}
	case menuOpenTraySettings:
		t.runAction(func() error { return t.app.OpenTraySettings() }, "Opened tray settings.", "Open tray settings failed")
	case menuOpenDataDir:
		t.runAction(func() error { return t.app.OpenDataDir() }, "Opened data directory.", "Open data directory failed")
	case menuOpenLogsDir:
		t.runAction(func() error { return t.app.OpenLogsDir() }, "Opened logs directory.", "Open logs directory failed")
	case menuQuit:
		t.requestQuitAndStopService()
	}
}

func (t *windowsTray) runAction(action func() error, success string, failure string) {
	// 异步执行一个菜单动作，并统一处理成功/失败提示。
	go func() {
		if err := action(); err != nil {
			t.ShowError("FeedMeDaily Tray", failure+": "+err.Error())
			return
		}
		if success != "" {
			t.ShowInfo("FeedMeDaily Tray", success)
		}
	}()
}

func (t *windowsTray) showContextMenu() {
	// 动态构建右键菜单，并在鼠标位置弹出。
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	state := t.app.MenuState()
	_ = appendMenu(menu, mfString, uintptr(menuOpenApp), "Open FeedMeDaily")
	_ = appendMenu(menu, mfString, uintptr(menuRunSync), "Run Sync Now")

	scheduleFlags := uint32(mfString)
	if state.ScheduleEnabled {
		scheduleFlags |= mfChecked
	}
	_ = appendMenu(menu, scheduleFlags, uintptr(menuToggleSchedule), t.app.SchedulerLabel())

	loginFlags := uint32(mfString)
	if state.LaunchAtLogin {
		loginFlags |= mfChecked
	}
	_ = appendMenu(menu, loginFlags, uintptr(menuLaunchAtLogin), "Launch At Login")
	_ = appendMenu(menu, mfString, uintptr(menuOpenTraySettings), "Open Tray Settings")
	_ = appendMenu(menu, mfSeparator, 0, "")
	_ = appendMenu(menu, mfString, uintptr(menuOpenDataDir), "Open Data Folder")
	_ = appendMenu(menu, mfString, uintptr(menuOpenLogsDir), "Open Logs Folder")
	_ = appendMenu(menu, mfSeparator, 0, "")
	_ = appendMenu(menu, mfString, uintptr(menuQuit), "Quit Tray And Stop Service")

	var cursor point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	procSetForegroundWnd.Call(t.hwnd)
	procTrackPopupMenu.Call(
		menu,
		tpmLeftAlign|tpmBottomAlign|tpmRightButton,
		uintptr(cursor.X),
		uintptr(cursor.Y),
		0,
		t.hwnd,
		0,
	)
	// 这是 Windows 托盘右键菜单的经典收尾动作，否则菜单状态有时会卡住。
	t.postTrayMessage(wmNull, 0, 0)
}

func (t *windowsTray) loadIcon() (uintptr, error) {
	// 优先加载项目自带图标，失败时退回系统默认应用图标。
	if t.app.layout.IconPath != "" {
		path, err := syscall.UTF16PtrFromString(t.app.layout.IconPath)
		if err == nil {
			icon, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(path)), imageIcon, 0, 0, lrLoadFromFile|lrDefaultSize)
			if icon != 0 {
				return icon, nil
			}
		}
	}

	icon, _, err := procLoadIconW.Call(0, uintptr(idiApplication))
	if icon == 0 {
		return 0, fmt.Errorf("load tray icon: %v", err)
	}
	return icon, nil
}

func windowProc(hwnd uintptr, message uint32, wParam uintptr, lParam uintptr) uintptr {
	// 统一处理托盘回调、菜单命令和窗口销毁事件。
	if globalTray == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return ret
	}

	switch message {
	case taskbarCreatedMessage:
		globalTray.log("info", "taskbar_created", "Received TaskbarCreated broadcast.", nil, nil)
		globalTray.requestRefreshIcon(false)
		return 0
	case trayCallbackMessage:
		switch uint32(lParam) {
		case wmLButtonDblClk:
			globalTray.log("info", "tray_callback", "Received tray callback.", map[string]any{"event": "WM_LBUTTONDBLCLK"}, nil)
			globalTray.handleCommand(menuOpenApp)
		case wmRButtonUp, wmContextMenu:
			event := "WM_RBUTTONUP"
			if uint32(lParam) == wmContextMenu {
				event = "WM_CONTEXTMENU"
			}
			globalTray.log("info", "tray_callback", "Received tray callback.", map[string]any{"event": event}, nil)
			globalTray.showContextMenu()
		}
		return 0
	case trayMsgRefreshIcon:
		globalTray.handleRefreshMessage(wParam != 0)
		return 0
	case trayMsgReloadSetting:
		globalTray.app.refreshSettingsFromDisk("settings_message_refresh_failed")
		return 0
	case trayMsgShowInfo, trayMsgShowError:
		globalTray.handleQueuedBalloon()
		return 0
	case wmPowerBroadcast:
		switch uint32(wParam) {
		case pbtApmResumeAutomatic:
			globalTray.log("info", "power_resume_automatic", "Received automatic resume broadcast.", nil, nil)
		case pbtApmResumeSuspend:
			globalTray.log("info", "power_resume_suspend", "Received resume-from-suspend broadcast.", nil, nil)
			globalTray.requestRefreshIcon(false)
		}
		return 1
	case wmCommand:
		globalTray.handleCommand(uint16(wParam & 0xffff))
		return 0
	case wmDestroy:
		globalTray.removeTrayIcon()
		procPostQuitMessage.Call(0)
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	default:
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return ret
	}
}

func registerWindowMessage(name string) uint32 {
	// 注册系统广播消息名，例如 TaskbarCreated，用来处理 shell 重建。
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0
	}
	value, _, _ := procRegisterWindowMsg.Call(uintptr(unsafe.Pointer(namePtr)))
	return uint32(value)
}

func (t *windowsTray) requestQuit() {
	// 通过给隐藏窗口发关闭消息来结束托盘循环。
	t.postTrayMessage(wmClose, 0, 0)
}

func (t *windowsTray) requestQuitAndStopService() {
	// 关闭托盘时统一收掉后台服务，避免留下孤儿进程。
	go func() {
		if err := t.app.StopService(); err != nil {
			t.ShowError("FeedMeDaily Tray", "Stop service failed: "+err.Error())
		}
		t.requestQuit()
	}()
}

func appendMenu(menu uintptr, flags uint32, itemID uintptr, label string) error {
	// 往 Windows 弹出菜单里追加一个菜单项。
	var labelPtr *uint16
	if label != "" {
		ptr, err := syscall.UTF16PtrFromString(label)
		if err != nil {
			return err
		}
		labelPtr = ptr
	}
	ok, _, err := procAppendMenuW.Call(menu, uintptr(flags), itemID, uintptr(unsafe.Pointer(labelPtr)))
	if ok == 0 {
		return fmt.Errorf("append menu: %v", err)
	}
	return nil
}

func copyUTF16ToFixedArray(target []uint16, value string) {
	// 把 Go 字符串复制到 Windows 固定长度 UTF-16 缓冲区。
	encoded, _ := syscall.UTF16FromString(value)
	if len(encoded) > len(target) {
		encoded = encoded[:len(target)]
		encoded[len(target)-1] = 0
	}
	copy(target, encoded)
}

func (t *windowsTray) handleQueuedBalloon() {
	// 由 UI 线程消费一条待显示的气泡消息。
	request, ok := t.dequeueBalloon()
	if !ok {
		return
	}
	t.showBalloon(request)
}

func (t *windowsTray) dequeueBalloon() (trayBalloonRequest, bool) {
	// 从线程安全队列取出下一条气泡消息。
	t.balloonMutex.Lock()
	defer t.balloonMutex.Unlock()
	if len(t.balloonQueue) == 0 {
		return trayBalloonRequest{}, false
	}
	request := t.balloonQueue[0]
	t.balloonQueue = t.balloonQueue[1:]
	return request, true
}

func (t *windowsTray) requestRefreshIcon(retry bool) {
	// 后台线程只请求一次刷新，实际刷新逻辑始终在 UI 线程执行。
	var retryValue uintptr
	if retry {
		retryValue = 1
	}
	if !t.postTrayMessage(trayMsgRefreshIcon, retryValue, 0) {
		t.log("error", "tray_refresh_post_failed", "Posting tray refresh message failed.", map[string]any{
			"retry": retry,
		}, nil)
	}
}

func (t *windowsTray) handleRefreshMessage(retry bool) {
	// 在 UI 线程刷新托盘图标，并在首次失败后做一次延迟重试。
	if retry {
		if !t.refreshRetryScheduled {
			return
		}
		t.refreshRetryScheduled = false
	}
	t.log("info", "tray_icon_refresh_started", "Refreshing tray icon.", map[string]any{
		"retry": retry,
	}, nil)
	if err := t.refreshTrayIcon(); err != nil {
		t.log("error", "tray_icon_refresh_failed", "Refreshing tray icon failed.", map[string]any{
			"retry": retry,
		}, err)
		if !retry && !t.refreshRetryScheduled {
			t.refreshRetryScheduled = true
			scheduleAfterFunc(refreshRetryDelay, func() {
				t.requestRefreshIcon(true)
			})
			return
		}
		t.ShowError("FeedMeDaily Tray", "Tray icon recovery failed: "+err.Error())
		return
	}
	t.refreshRetryScheduled = false
	t.log("info", "tray_icon_refresh_succeeded", "Refreshed tray icon.", map[string]any{
		"retry": retry,
	}, nil)
}

func (t *windowsTray) postTrayMessage(message uint32, wParam uintptr, lParam uintptr) bool {
	// 统一封装 PostMessage，便于测试和后台线程调度。
	if t.hwnd == 0 {
		return false
	}
	return postMessageCall(t.hwnd, message, wParam, lParam)
}

func (t *windowsTray) log(level string, action string, message string, data map[string]any, err error) {
	// 给托盘恢复和回调路径补上最小但关键的日志。
	event := logging.Event{
		Level:     level,
		Component: "tray",
		Action:    action,
		Message:   message,
		Data:      data,
	}
	if err != nil {
		event.Error = err.Error()
	}
	_, _ = logging.Write(t.app.layout.LogsDir, event)
}
