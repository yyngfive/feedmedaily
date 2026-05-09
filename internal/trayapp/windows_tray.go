//go:build windows

package trayapp

import (
	"fmt"
	"syscall"
	"unsafe"
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
	niifError          = 0x00000003
	niifInfo           = 0x00000001
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
	wmRButtonUp        = 0x0205
	wmUser             = 0x0400
	wsOverlappedWindow = 0x00CF0000
)

const (
	menuOpenApp uint16 = 1001 + iota
	menuRunSync
	menuStartService
	menuStopService
	menuToggleSchedule
	menuLaunchAtLogin
	menuOpenTraySettings
	menuOpenDataDir
	menuOpenLogsDir
	menuQuitAndStopService
	menuQuit
)

const trayCallbackMessage = wmApp + 1

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	shell32              = syscall.NewLazyDLL("shell32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procCreateMutexW     = kernel32.NewProc("CreateMutexW")
	procCloseHandle      = kernel32.NewProc("CloseHandle")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procLoadIconW        = user32.NewProc("LoadIconW")
	procLoadImageW       = user32.NewProc("LoadImageW")
	procMessageBoxW      = user32.NewProc("MessageBoxW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procSetForegroundWnd = user32.NewProc("SetForegroundWindow")
	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
)

var globalTray *windowsTray

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

type windowsTray struct {
	app         *App
	hwnd        uintptr
	icon        uintptr
	mutexHandle uintptr
	nid         notifyIconData
}

func newWindowsTray(app *App) (*windowsTray, error) {
	mutexName, err := syscall.UTF16PtrFromString("FeedMeDailyTrayMutex")
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
	globalTray = t
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

	if t.mutexHandle != 0 {
		procCloseHandle.Call(t.mutexHandle)
	}

	return nil
}

func (t *windowsTray) addTrayIcon() error {
	t.nid = notifyIconData{
		Size:        uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:        t.hwnd,
		ID:          1,
		Flags:       nifMessage | nifIcon | nifTip,
		CallbackMsg: trayCallbackMessage,
		Icon:        t.icon,
	}
	copyUTF16ToFixedArray(t.nid.Tip[:], "FeedMeDaily Tray")
	if ok, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid))); ok == 0 {
		return fmt.Errorf("add tray icon: %v", err)
	}
	return nil
}

func (t *windowsTray) removeTrayIcon() {
	if t.hwnd != 0 {
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
	}
}

func (t *windowsTray) ShowInfo(title string, body string) {
	t.showBalloon(title, body, niifInfo)
}

func (t *windowsTray) ShowError(title string, body string) {
	t.showBalloon(title, body, niifError)
}

func (t *windowsTray) showBalloon(title string, body string, infoFlags uint32) {
	t.nid.Flags = nifInfo
	copyUTF16ToFixedArray(t.nid.InfoTitle[:], title)
	copyUTF16ToFixedArray(t.nid.Info[:], body)
	t.nid.InfoFlags = infoFlags
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
	t.nid.Flags = nifMessage | nifIcon | nifTip
}

func (t *windowsTray) handleCommand(commandID uint16) {
	switch commandID {
	case menuOpenApp:
		t.runAction(func() error { return t.app.OpenApp() }, "Opened FeedMeDaily.", "Open FeedMeDaily failed")
	case menuRunSync:
		t.runAction(func() error { return t.app.RunSyncNow() }, "Sync job started.", "Run sync failed")
	case menuStartService:
		t.runAction(func() error { return t.app.StartService() }, "Background service started.", "Start service failed")
	case menuStopService:
		t.runAction(func() error { return t.app.StopService() }, "Background service stopped.", "Stop service failed")
	case menuToggleSchedule:
		go func() {
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
		}()
	case menuLaunchAtLogin:
		go func() {
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
		}()
	case menuOpenTraySettings:
		t.runAction(func() error { return t.app.OpenTraySettings() }, "Opened tray settings.", "Open tray settings failed")
	case menuOpenDataDir:
		t.runAction(func() error { return t.app.OpenDataDir() }, "Opened data directory.", "Open data directory failed")
	case menuOpenLogsDir:
		t.runAction(func() error { return t.app.OpenLogsDir() }, "Opened logs directory.", "Open logs directory failed")
	case menuQuitAndStopService:
		go func() {
			if err := t.app.StopService(); err != nil {
				t.ShowError("FeedMeDaily Tray", "Stop service failed: "+err.Error())
				return
			}
			t.requestQuit()
		}()
	case menuQuit:
		t.requestQuit()
	}
}

func (t *windowsTray) runAction(action func() error, success string, failure string) {
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
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	state := t.app.MenuState()
	_ = appendMenu(menu, mfString, uintptr(menuOpenApp), "Open FeedMeDaily")
	_ = appendMenu(menu, mfString, uintptr(menuRunSync), "Run Sync Now")
	_ = appendMenu(menu, mfSeparator, 0, "")
	_ = appendMenu(menu, mfString, uintptr(menuStartService), "Start Background Service")
	_ = appendMenu(menu, mfString, uintptr(menuStopService), "Stop Background Service")

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
	_ = appendMenu(menu, mfString, uintptr(menuQuitAndStopService), "Quit Tray And Stop Service")
	_ = appendMenu(menu, mfString, uintptr(menuQuit), "Quit Tray")

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
}

func (t *windowsTray) loadIcon() (uintptr, error) {
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
	if globalTray == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return ret
	}

	switch message {
	case trayCallbackMessage:
		switch uint32(lParam) {
		case wmLButtonDblClk:
			globalTray.handleCommand(menuOpenApp)
		case wmRButtonUp, wmContextMenu:
			globalTray.showContextMenu()
		}
		return 0
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

func (t *windowsTray) requestQuit() {
	procPostMessageW.Call(t.hwnd, wmClose, 0, 0)
}

func appendMenu(menu uintptr, flags uint32, itemID uintptr, label string) error {
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
	encoded, _ := syscall.UTF16FromString(value)
	if len(encoded) > len(target) {
		encoded = encoded[:len(target)]
		encoded[len(target)-1] = 0
	}
	copy(target, encoded)
}
