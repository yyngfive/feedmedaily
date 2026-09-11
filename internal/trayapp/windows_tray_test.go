//go:build windows

package trayapp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"testing"
)

type postedTrayMessage struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
}

func TestAddTrayIconSetsVersionAfterAdd(t *testing.T) {
	tray := &windowsTray{
		app:  &App{},
		hwnd: 101,
		icon: 202,
	}

	var calls []uint32
	restore := replaceShellNotifyIconCall(func(message uint32, data *notifyIconData) (bool, error) {
		calls = append(calls, message)
		if data.VersionOrTimer != notifyIconVersion {
			t.Fatalf("VersionOrTimer = %d, want %d", data.VersionOrTimer, notifyIconVersion)
		}
		return true, nil
	})
	defer restore()

	if err := tray.addTrayIcon(); err != nil {
		t.Fatal(err)
	}

	want := []uint32{nimAdd, nimSetVersion}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestShowInfoPostsInternalMessageInsteadOfMutatingTrayIcon(t *testing.T) {
	tray := &windowsTray{
		app:  &App{},
		hwnd: 303,
		nid: notifyIconData{
			Flags: nifMessage | nifIcon | nifTip,
		},
	}

	var posted []postedTrayMessage
	restorePost := replacePostMessageCall(func(hwnd uintptr, message uint32, wParam uintptr, lParam uintptr) bool {
		posted = append(posted, postedTrayMessage{
			hwnd:    hwnd,
			message: message,
			wParam:  wParam,
			lParam:  lParam,
		})
		return true
	})
	defer restorePost()

	shellCalled := false
	restoreShell := replaceShellNotifyIconCall(func(message uint32, data *notifyIconData) (bool, error) {
		shellCalled = true
		return true, nil
	})
	defer restoreShell()

	tray.ShowInfo("FeedMeDaily Tray", "Opened FeedMeDaily.")

	if shellCalled {
		t.Fatal("ShowInfo should not call Shell_NotifyIcon directly")
	}
	if tray.nid.Flags != nifMessage|nifIcon|nifTip {
		t.Fatalf("nid.Flags = %d, want %d", tray.nid.Flags, nifMessage|nifIcon|nifTip)
	}
	wantPosted := []postedTrayMessage{{
		hwnd:    303,
		message: trayMsgShowInfo,
		wParam:  0,
		lParam:  0,
	}}
	if !reflect.DeepEqual(posted, wantPosted) {
		t.Fatalf("posted = %#v, want %#v", posted, wantPosted)
	}

	request, ok := tray.dequeueBalloon()
	if !ok {
		t.Fatal("expected one queued balloon request")
	}
	if request.title != "FeedMeDaily Tray" || request.body != "Opened FeedMeDaily." || request.infoFlags != niifInfo {
		t.Fatalf("request = %#v", request)
	}
}

func TestWindowProcResumeAutomaticDoesNotRequestRefresh(t *testing.T) {
	tray := &windowsTray{
		app:  &App{},
		hwnd: 404,
	}
	restoreGlobal := replaceGlobalTray(tray)
	defer restoreGlobal()

	var posted []postedTrayMessage
	restorePost := replacePostMessageCall(func(hwnd uintptr, message uint32, wParam uintptr, lParam uintptr) bool {
		posted = append(posted, postedTrayMessage{
			hwnd:    hwnd,
			message: message,
			wParam:  wParam,
			lParam:  lParam,
		})
		return true
	})
	defer restorePost()

	result := windowProc(tray.hwnd, wmPowerBroadcast, uintptr(pbtApmResumeAutomatic), 0)
	if result != 1 {
		t.Fatalf("result = %d, want 1", result)
	}
	if len(posted) != 0 {
		t.Fatalf("posted = %#v, want no tray refresh message", posted)
	}
}

func TestWindowProcResumeSuspendRequestsRefresh(t *testing.T) {
	tray := &windowsTray{
		app:  &App{},
		hwnd: 505,
	}
	restoreGlobal := replaceGlobalTray(tray)
	defer restoreGlobal()

	var posted []postedTrayMessage
	restorePost := replacePostMessageCall(func(hwnd uintptr, message uint32, wParam uintptr, lParam uintptr) bool {
		posted = append(posted, postedTrayMessage{
			hwnd:    hwnd,
			message: message,
			wParam:  wParam,
			lParam:  lParam,
		})
		return true
	})
	defer restorePost()

	result := windowProc(tray.hwnd, wmPowerBroadcast, uintptr(pbtApmResumeSuspend), 0)
	if result != 1 {
		t.Fatalf("result = %d, want 1", result)
	}
	wantPosted := []postedTrayMessage{{
		hwnd:    505,
		message: trayMsgRefreshIcon,
		wParam:  0,
		lParam:  0,
	}}
	if !reflect.DeepEqual(posted, wantPosted) {
		t.Fatalf("posted = %#v, want %#v", posted, wantPosted)
	}
}

func TestNotifySettingsChangedPostsReloadMessageToTrayWindow(t *testing.T) {
	configDir := t.TempDir()
	var seenConfigDir string
	restoreFind := replaceFindTrayWindowCall(func(configDir string) uintptr {
		seenConfigDir = configDir
		return 606
	})
	defer restoreFind()

	var posted []postedTrayMessage
	restorePost := replacePostMessageCall(func(hwnd uintptr, message uint32, wParam uintptr, lParam uintptr) bool {
		posted = append(posted, postedTrayMessage{
			hwnd:    hwnd,
			message: message,
			wParam:  wParam,
			lParam:  lParam,
		})
		return true
	})
	defer restorePost()

	if err := NotifySettingsChanged(configDir); err != nil {
		t.Fatal(err)
	}
	if seenConfigDir != configDir {
		t.Fatalf("find configDir = %q, want %q", seenConfigDir, configDir)
	}
	wantPosted := []postedTrayMessage{{
		hwnd:    606,
		message: trayMsgReloadSetting,
		wParam:  0,
		lParam:  0,
	}}
	if !reflect.DeepEqual(posted, wantPosted) {
		t.Fatalf("posted = %#v, want %#v", posted, wantPosted)
	}
}

func TestShutdownRunningAppStopsServiceBeforeClosingTray(t *testing.T) {
	root := t.TempDir()
	layout := testLayout(root, runtimeModeRelease)

	var exitCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/app/health":
			if r.Method != http.MethodGet {
				t.Errorf("health method = %s, want GET", r.Method)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/api/app/exit":
			if r.Method != http.MethodPost {
				t.Errorf("exit method = %s, want POST", r.Method)
				return
			}
			exitCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(serverURL.Port())
	if err != nil {
		t.Fatal(err)
	}
	layout.ServerHost = serverURL.Hostname()
	layout.ServerPort = port
	if err := WriteRuntimeState(layout.RuntimeStatePath, RuntimeState{PID: 4242, Port: port}); err != nil {
		t.Fatal(err)
	}

	runningChecks := 0
	restoreProcess := replaceProcessRunningCall(func(pid int) bool {
		if pid != 4242 {
			t.Fatalf("ProcessRunning pid = %d, want 4242", pid)
		}
		runningChecks++
		return runningChecks == 1
	})
	defer restoreProcess()

	findCalls := 0
	restoreFind := replaceFindTrayWindowCall(func(configDir string) uintptr {
		if configDir != layout.ConfigDir {
			t.Fatalf("find configDir = %q, want %q", configDir, layout.ConfigDir)
		}
		findCalls++
		if findCalls == 1 {
			return 909
		}
		return 0
	})
	defer restoreFind()

	var posted []postedTrayMessage
	restorePost := replacePostMessageCall(func(hwnd uintptr, message uint32, wParam uintptr, lParam uintptr) bool {
		if !exitCalled {
			t.Error("tray close was requested before the service exit request completed")
		}
		posted = append(posted, postedTrayMessage{
			hwnd:    hwnd,
			message: message,
			wParam:  wParam,
			lParam:  lParam,
		})
		return true
	})
	defer restorePost()

	if err := shutdownRunningApp(layout); err != nil {
		t.Fatal(err)
	}
	if !exitCalled {
		t.Fatal("expected the running service to receive an exit request")
	}
	wantPosted := []postedTrayMessage{{
		hwnd:    909,
		message: wmClose,
		wParam:  0,
		lParam:  0,
	}}
	if !reflect.DeepEqual(posted, wantPosted) {
		t.Fatalf("posted = %#v, want %#v", posted, wantPosted)
	}
	if _, err := os.Stat(layout.RuntimeStatePath); !os.IsNotExist(err) {
		t.Fatalf("runtime state error = %v, want file to be removed", err)
	}
}

func TestWindowProcReloadSettingsRefreshesAppState(t *testing.T) {
	app, settingsPath := testSchedulerApp(t, TraySettings{
		ScheduleEnabled: false,
		DailyTime:       "10:30",
		LaunchAtLogin:   false,
	})
	writeTraySettings(t, settingsPath, TraySettings{
		ScheduleEnabled: true,
		DailyTime:       "08:15",
		LaunchAtLogin:   false,
	})
	tray := &windowsTray{
		app:  app,
		hwnd: 707,
	}
	app.tray = tray
	restoreGlobal := replaceGlobalTray(tray)
	defer restoreGlobal()

	var posted []postedTrayMessage
	restorePost := replacePostMessageCall(func(hwnd uintptr, message uint32, wParam uintptr, lParam uintptr) bool {
		posted = append(posted, postedTrayMessage{
			hwnd:    hwnd,
			message: message,
			wParam:  wParam,
			lParam:  lParam,
		})
		return true
	})
	defer restorePost()

	result := windowProc(tray.hwnd, trayMsgReloadSetting, 0, 0)
	if result != 0 {
		t.Fatalf("result = %d, want 0", result)
	}
	state := app.currentMenuStateForTest()
	if !state.ScheduleEnabled || state.DailyTime != "08:15" {
		t.Fatalf("state = %#v", state)
	}
	wantPosted := []postedTrayMessage{{
		hwnd:    707,
		message: trayMsgRefreshIcon,
		wParam:  0,
		lParam:  0,
	}}
	if !reflect.DeepEqual(posted, wantPosted) {
		t.Fatalf("posted = %#v, want %#v", posted, wantPosted)
	}
}

func replaceShellNotifyIconCall(next func(uint32, *notifyIconData) (bool, error)) func() {
	previous := shellNotifyIconCall
	shellNotifyIconCall = next
	return func() {
		shellNotifyIconCall = previous
	}
}

func replaceFindTrayWindowCall(next func(string) uintptr) func() {
	previous := findTrayWindowCall
	findTrayWindowCall = next
	return func() {
		findTrayWindowCall = previous
	}
}

func replacePostMessageCall(next func(uintptr, uint32, uintptr, uintptr) bool) func() {
	previous := postMessageCall
	postMessageCall = next
	return func() {
		postMessageCall = previous
	}
}

func replaceGlobalTray(next *windowsTray) func() {
	previous := globalTray
	globalTray = next
	return func() {
		globalTray = previous
	}
}
