//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

const (
	windowWidth  = 1240
	windowHeight = 920
	statusHeight = 64

	wmDestroy     = 0x0002
	wmSize        = 0x0005
	wmClose       = 0x0010
	wmAppAction   = 0x8001
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsThickFrame  = 0x00040000
	wsMinimizeBox = 0x00020000
	wsMaximizeBox = 0x00010000
	wsVisible     = 0x10000000
	wsChild       = 0x40000000
	wsExTopmost   = 0x00000008
	swShow        = 5
	colorWindow   = 5
	cwUseDefault  = ^uintptr(0x7fffffff)
	maxNavRetries = 3
	idcArrow      = 32512
	hwndTopmost   = ^uintptr(0)
	hwndNoTopmost = ^uintptr(1)
	swpNoMove     = 0x0002
	swpNoSize     = 0x0001
	swpShowWindow = 0x0040
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procSetForegroundWin = user32.NewProc("SetForegroundWindow")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

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
	IconSm     uintptr
}

type verifierApp struct {
	opts     cliOptions
	log      verifierLogger
	hwnd     uintptr
	status   uintptr
	chromium *edge.Chromium

	responseHandler *webResourceResponseReceivedHandler
	contentHandlers []*responseContentHandler

	mu                sync.Mutex
	actions           []func()
	remaining         []string
	currentFeedURL    string
	capturedFeeds     map[string]capturedFeed
	approvalRefresh   map[string]bool
	navigationRetries map[string]int
	completionPosted  bool
	needsUserPosted   bool
}

var activeApp *verifierApp

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	app := newVerifierApp(opts)
	if err := app.run(); err != nil {
		app.log.Printf("startup failed: %s", err)
		_ = app.postTerminalResult("failed", false, err.Error())
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newVerifierApp(opts cliOptions) *verifierApp {
	return &verifierApp{
		opts:              opts,
		log:               newVerifierLogger(opts),
		remaining:         append([]string(nil), opts.FeedURLs...),
		capturedFeeds:     map[string]capturedFeed{},
		approvalRefresh:   map[string]bool{},
		navigationRetries: map[string]int{},
	}
}

func (a *verifierApp) run() error {
	activeApp = a
	a.log.Printf("started verification_id=%s host=%s feeds=%d", a.opts.VerificationID, a.opts.VerificationHost, len(a.opts.FeedURLs))
	if err := os.MkdirAll(a.opts.UserDataDir, 0o755); err != nil {
		return err
	}
	hwnd, err := a.createWindow()
	if err != nil {
		return err
	}
	a.hwnd = hwnd
	a.setStatus("FeedMeDaily is opening protected feeds in a persistent WebView2 profile. If Cloudflare asks for a human check, complete it here and leave the window open while the remaining feeds load.")

	chromium := edge.NewChromium()
	chromium.DataPath = a.opts.UserDataDir
	chromium.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationCompletedEventArgs) {
		a.onNavigationCompleted(uintptr(unsafe.Pointer(args)))
	}
	chromium.SetErrorCallback(func(err error) {
		a.enqueue(func() { a.failAndClose(err.Error()) })
	})
	a.chromium = chromium

	procShowWindow.Call(hwnd, swShow)
	procSetForegroundWin.Call(hwnd)
	procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
	time.AfterFunc(1200*time.Millisecond, func() {
		a.enqueue(func() {
			procSetWindowPos.Call(hwnd, hwndNoTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
		})
	})

	if !chromium.Embed(hwnd) {
		return fmt.Errorf("embed WebView2")
	}
	chromium.SetPadding(edge.Rect{Top: statusHeight})
	if err := a.registerResponseHandler(); err != nil {
		return err
	}
	time.AfterFunc(60*time.Second, func() {
		a.enqueue(func() { a.postNeedsUserIfNeeded("watchdog fired") })
	})
	a.navigateNextFeed()
	a.messageLoop()
	return nil
}

func (a *verifierApp) createWindow() (uintptr, error) {
	className, _ := windows.UTF16PtrFromString("FeedMeDailyProtectedVerifierWindow")
	title, _ := windows.UTF16PtrFromString("Protected Feed Verification")
	hinst, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    windows.NewCallback(wndProc),
		Instance:   hinst,
		Cursor:     cursor,
		Background: colorWindow + 1,
		ClassName:  className,
	}
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return 0, fmt.Errorf("register verifier window class: %w", err)
	}
	style := uintptr(wsCaption | wsSysMenu | wsThickFrame | wsMinimizeBox | wsMaximizeBox | wsVisible)
	hwnd, _, err := procCreateWindowExW.Call(
		wsExTopmost,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		style,
		cwUseDefault,
		cwUseDefault,
		windowWidth,
		windowHeight,
		0,
		0,
		hinst,
		0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("create verifier window: %w", err)
	}
	staticClass, _ := windows.UTF16PtrFromString("STATIC")
	statusText, _ := windows.UTF16PtrFromString("")
	a.status, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(statusText)),
		wsChild|wsVisible,
		0,
		0,
		windowWidth,
		statusHeight,
		hwnd,
		0,
		hinst,
		0,
	)
	return hwnd, nil
}

func (a *verifierApp) messageLoop() {
	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (a *verifierApp) enqueue(fn func()) {
	a.mu.Lock()
	a.actions = append(a.actions, fn)
	a.mu.Unlock()
	if a.hwnd != 0 {
		procPostMessageW.Call(a.hwnd, wmAppAction, 0, 0)
	}
}

func (a *verifierApp) drainActions() {
	a.mu.Lock()
	actions := append([]func(){}, a.actions...)
	a.actions = nil
	a.mu.Unlock()
	for _, action := range actions {
		action()
	}
}

func (a *verifierApp) setStatus(text string) {
	if a.status == 0 {
		return
	}
	ptr, _ := windows.UTF16PtrFromString(text)
	procSetWindowTextW.Call(a.status, uintptr(unsafe.Pointer(ptr)))
}

func (a *verifierApp) navigateNextFeed() {
	if len(a.remaining) == 0 {
		a.completeAndClose()
		return
	}
	a.currentFeedURL = a.remaining[0]
	a.remaining = a.remaining[1:]
	a.setStatus(fmt.Sprintf("Opening protected feed %d/%d. If Cloudflare appears, complete the check and keep this window open.", len(a.capturedFeeds)+1, len(a.opts.FeedURLs)))
	a.log.Printf("navigate feed=%s", a.currentFeedURL)
	a.chromium.Navigate(a.currentFeedURL)
}

func (a *verifierApp) registerResponseHandler() error {
	webview := chromiumWebView(a.chromium)
	if webview == 0 {
		return fmt.Errorf("WebView2 core was not initialized")
	}
	webview2v2 := queryInterface(webview, "{9E8F0CF8-E670-4B5E-B2BC-73E061E3184C}")
	if webview2v2 == 0 {
		return fmt.Errorf("WebView2 runtime does not expose WebResourceResponseReceived")
	}
	a.responseHandler = newWebResourceResponseReceivedHandler(a)
	var token eventRegistrationToken
	hr, _, _ := ((*iCoreWebView2_2)(unsafe.Pointer(webview2v2))).vtbl.AddWebResourceResponseReceived.Call(
		webview2v2,
		uintptr(unsafe.Pointer(a.responseHandler)),
		uintptr(unsafe.Pointer(&token)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return syscall.Errno(hr)
	}
	return nil
}

func (a *verifierApp) onNavigationCompleted(args uintptr) {
	success := int32(0)
	navArgs := (*navigationCompletedEventArgs)(unsafe.Pointer(args))
	hr, _, _ := navArgs.vtbl.GetIsSuccess.Call(args, uintptr(unsafe.Pointer(&success)))
	if windows.Handle(hr) != windows.S_OK {
		a.log.Printf("navigation status unavailable feed=%s error=0x%x", a.currentFeedURL, hr)
		return
	}
	if success != 0 {
		a.log.Printf("navigation completed feed=%s", a.currentFeedURL)
		if a.needsUserPosted {
			a.setStatus("Cloudflare approval received. FeedMeDaily is now collecting the remaining protected-feed XML documents.")
			a.refreshAfterApproval(a.currentFeedURL)
		} else {
			a.setStatus("Checking whether this protected feed now resolves to XML.")
		}
		return
	}
	a.log.Printf("navigation failed feed=%s", a.currentFeedURL)
	a.setStatus("The page has not fully loaded yet. If Cloudflare appears, complete the human verification and keep the window open.")
	a.retryNavigation(a.currentFeedURL)
}

func (a *verifierApp) refreshAfterApproval(feedURL string) {
	if strings.TrimSpace(feedURL) == "" {
		return
	}
	a.mu.Lock()
	if a.completionPosted || a.approvalRefresh[feedURL] {
		a.mu.Unlock()
		return
	}
	if _, ok := a.capturedFeeds[feedURL]; ok {
		a.mu.Unlock()
		return
	}
	a.approvalRefresh[feedURL] = true
	a.mu.Unlock()

	time.AfterFunc(900*time.Millisecond, func() {
		a.enqueue(func() {
			a.mu.Lock()
			_, captured := a.capturedFeeds[feedURL]
			shouldRefresh := !a.completionPosted && !captured && a.currentFeedURL == feedURL
			a.mu.Unlock()
			if !shouldRefresh {
				return
			}
			a.log.Printf("refresh after approval feed=%s", feedURL)
			a.chromium.Navigate(feedURL)
		})
	})
}

func (a *verifierApp) retryNavigation(feedURL string) {
	if strings.TrimSpace(feedURL) == "" {
		return
	}
	a.mu.Lock()
	if a.completionPosted {
		a.mu.Unlock()
		return
	}
	if _, ok := a.capturedFeeds[feedURL]; ok {
		a.mu.Unlock()
		return
	}
	a.navigationRetries[feedURL]++
	attempt := a.navigationRetries[feedURL]
	a.mu.Unlock()
	if attempt > maxNavRetries {
		a.log.Printf("navigation retry limit reached feed=%s attempts=%d", feedURL, attempt-1)
		return
	}
	time.AfterFunc(time.Duration(attempt*4)*time.Second, func() {
		a.enqueue(func() {
			a.mu.Lock()
			_, captured := a.capturedFeeds[feedURL]
			shouldRetry := !a.completionPosted && !captured && a.currentFeedURL == feedURL
			a.mu.Unlock()
			if !shouldRetry {
				return
			}
			a.log.Printf("retry navigation feed=%s attempt=%d/%d", feedURL, attempt, maxNavRetries)
			a.chromium.Navigate(feedURL)
		})
	})
}

func (a *verifierApp) onResponseReceived(args uintptr) {
	a.mu.Lock()
	if a.completionPosted || a.currentFeedURL == "" {
		a.mu.Unlock()
		return
	}
	currentFeed := a.currentFeedURL
	if _, ok := a.capturedFeeds[currentFeed]; ok {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	eventArgs := (*webResourceResponseReceivedEventArgs)(unsafe.Pointer(args))
	var request *webResourceRequest
	hr, _, _ := eventArgs.vtbl.GetRequest.Call(args, uintptr(unsafe.Pointer(&request)))
	if windows.Handle(hr) != windows.S_OK || request == nil {
		return
	}
	requestURI := request.getURI()
	if !strings.EqualFold(strings.TrimSpace(requestURI), currentFeed) {
		return
	}
	var response *webResourceResponseView
	hr, _, _ = eventArgs.vtbl.GetResponse.Call(args, uintptr(unsafe.Pointer(&response)))
	if windows.Handle(hr) != windows.S_OK || response == nil {
		a.log.Printf("response unavailable feed=%s error=0x%x", currentFeed, hr)
		return
	}
	contentType := response.header("Content-Type")
	handler := newResponseContentHandler(a, currentFeed, contentType)
	a.mu.Lock()
	a.contentHandlers = append(a.contentHandlers, handler)
	a.mu.Unlock()
	hr, _, _ = response.vtbl.GetContent.Call(uintptr(unsafe.Pointer(response)), uintptr(unsafe.Pointer(handler)))
	if windows.Handle(hr) != windows.S_OK {
		a.log.Printf("response content unavailable feed=%s error=0x%x", currentFeed, hr)
	}
}

func (a *verifierApp) onResponseBody(feedURL, contentType, body string) {
	a.log.Printf("response feed=%s content_type=%s bytes=%d", feedURL, contentType, len(body))
	if looksLikeXML(contentType, body) {
		a.mu.Lock()
		if _, ok := a.capturedFeeds[feedURL]; !ok {
			a.capturedFeeds[feedURL] = capturedFeed{FeedURL: feedURL, ContentType: contentType, FeedXML: body}
		}
		count := len(a.capturedFeeds)
		a.mu.Unlock()
		a.log.Printf("captured xml feed=%s captured=%d/%d", feedURL, count, len(a.opts.FeedURLs))
		a.enqueue(func() {
			a.setStatus(fmt.Sprintf("Captured %d/%d protected-feed XML documents.", count, len(a.opts.FeedURLs)))
			a.navigateNextFeed()
		})
		return
	}
	if looksLikeChallenge(contentType, body) {
		a.log.Printf("challenge detected feed=%s", feedURL)
		a.enqueue(func() {
			a.setStatus("Cloudflare still needs a human check in this window. Complete it once and FeedMeDaily will keep trying the remaining protected feeds automatically.")
			a.postNeedsUserIfNeeded("")
		})
	}
}

func (a *verifierApp) postNeedsUserIfNeeded(reason string) {
	a.mu.Lock()
	if a.completionPosted || a.needsUserPosted {
		a.mu.Unlock()
		return
	}
	a.needsUserPosted = true
	feedURL := a.currentFeedURL
	a.mu.Unlock()
	if reason != "" {
		a.log.Printf("needs_user %s feed=%s", reason, feedURL)
	}
	payload := callbackPayload{
		VerificationID:   a.opts.VerificationID,
		VerificationHost: a.opts.VerificationHost,
		FeedURL:          feedURL,
		Status:           "needs_user",
		ContentType:      "application/xml",
		SessionVerified:  false,
		CapturedFeeds:    []capturedFeed{},
	}
	a.log.Printf("post needs_user feed=%s", feedURL)
	statusCode, status, err := postPayload(a.opts.CallbackURL, payload)
	if err != nil {
		a.log.Printf("callback failed: %s", err)
		return
	}
	a.log.Printf("callback status=%d reason=%s", statusCode, status)
}

func (a *verifierApp) completeAndClose() {
	a.mu.Lock()
	capturedCount := len(a.capturedFeeds)
	a.mu.Unlock()
	if capturedCount == 0 {
		a.failAndClose("the protected-feed verifier did not capture any feed XML")
		return
	}
	a.log.Printf("complete captured=%d", capturedCount)
	_ = a.postTerminalResult("success", true, "")
	procDestroyWindow.Call(a.hwnd)
}

func (a *verifierApp) failAndClose(message string) {
	a.log.Printf("failed error=%s", message)
	_ = a.postTerminalResult("failed", len(a.capturedFeeds) > 0, message)
	procDestroyWindow.Call(a.hwnd)
}

func (a *verifierApp) onClose() {
	a.mu.Lock()
	alreadyDone := a.completionPosted
	captured := len(a.capturedFeeds) > 0
	a.mu.Unlock()
	if !alreadyDone {
		a.log.Printf("window closed before completion")
		_ = a.postTerminalResult("aborted", captured, "the protected-feed verification window was closed before all feed XML was captured")
	}
	procDestroyWindow.Call(a.hwnd)
}

func (a *verifierApp) postTerminalResult(status string, sessionVerified bool, errorMessage string) error {
	a.mu.Lock()
	if a.completionPosted {
		a.mu.Unlock()
		return nil
	}
	a.completionPosted = true
	feedURL := a.currentFeedURL
	captured := orderedCapturedFeeds(a.capturedFeeds)
	a.mu.Unlock()
	payload := callbackPayload{
		VerificationID:   a.opts.VerificationID,
		VerificationHost: a.opts.VerificationHost,
		FeedURL:          feedURL,
		Status:           status,
		ContentType:      "application/xml",
		Error:            errorMessage,
		SessionVerified:  sessionVerified,
		CapturedFeeds:    captured,
	}
	a.log.Printf("post terminal status=%s session_verified=%t captured=%d error=%s", status, sessionVerified, len(captured), errorMessage)
	statusCode, responseStatus, err := postPayload(a.opts.CallbackURL, payload)
	if err != nil {
		a.log.Printf("callback failed: %s", err)
		return err
	}
	a.log.Printf("callback status=%d reason=%s", statusCode, responseStatus)
	return nil
}

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	if activeApp != nil {
		switch msg {
		case wmSize:
			if activeApp.chromium != nil {
				activeApp.chromium.Resize()
			}
			return 0
		case wmAppAction:
			activeApp.drainActions()
			return 0
		case wmClose:
			activeApp.onClose()
			return 0
		case wmDestroy:
			procPostQuitMessage.Call(0)
			return 0
		}
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}
