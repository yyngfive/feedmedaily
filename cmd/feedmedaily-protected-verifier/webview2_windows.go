//go:build windows

package main

import (
	"io"
	"reflect"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

func chromiumWebView(chromium *edge.Chromium) uintptr {
	value := reflect.ValueOf(chromium).Elem().FieldByName("webview")
	return *(*uintptr)(unsafe.Pointer(value.UnsafeAddr()))
}

func queryInterface(this uintptr, guid string) uintptr {
	unknown := (*iUnknown)(unsafe.Pointer(this))
	iid := edgeGUID(guid)
	var result uintptr
	hr, _, _ := unknown.vtbl.QueryInterface.Call(this, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&result)))
	if windows.Handle(hr) != windows.S_OK {
		return 0
	}
	return result
}

type eventRegistrationToken struct {
	value int64
}

type iUnknownVtbl struct {
	QueryInterface edge.ComProc
	AddRef         edge.ComProc
	Release        edge.ComProc
}

type iUnknown struct {
	vtbl *iUnknownVtbl
}

type iCoreWebView2_2Vtbl struct {
	iUnknownVtbl
	GetSettings                            edge.ComProc
	GetSource                              edge.ComProc
	Navigate                               edge.ComProc
	NavigateToString                       edge.ComProc
	AddNavigationStarting                  edge.ComProc
	RemoveNavigationStarting               edge.ComProc
	AddContentLoading                      edge.ComProc
	RemoveContentLoading                   edge.ComProc
	AddSourceChanged                       edge.ComProc
	RemoveSourceChanged                    edge.ComProc
	AddHistoryChanged                      edge.ComProc
	RemoveHistoryChanged                   edge.ComProc
	AddNavigationCompleted                 edge.ComProc
	RemoveNavigationCompleted              edge.ComProc
	AddFrameNavigationStarting             edge.ComProc
	RemoveFrameNavigationStarting          edge.ComProc
	AddFrameNavigationCompleted            edge.ComProc
	RemoveFrameNavigationCompleted         edge.ComProc
	AddScriptDialogOpening                 edge.ComProc
	RemoveScriptDialogOpening              edge.ComProc
	AddPermissionRequested                 edge.ComProc
	RemovePermissionRequested              edge.ComProc
	AddProcessFailed                       edge.ComProc
	RemoveProcessFailed                    edge.ComProc
	AddScriptToExecuteOnDocumentCreated    edge.ComProc
	RemoveScriptToExecuteOnDocumentCreated edge.ComProc
	ExecuteScript                          edge.ComProc
	CapturePreview                         edge.ComProc
	Reload                                 edge.ComProc
	PostWebMessageAsJSON                   edge.ComProc
	PostWebMessageAsString                 edge.ComProc
	AddWebMessageReceived                  edge.ComProc
	RemoveWebMessageReceived               edge.ComProc
	CallDevToolsProtocolMethod             edge.ComProc
	GetBrowserProcessID                    edge.ComProc
	GetCanGoBack                           edge.ComProc
	GetCanGoForward                        edge.ComProc
	GoBack                                 edge.ComProc
	GoForward                              edge.ComProc
	GetDevToolsProtocolEventReceiver       edge.ComProc
	Stop                                   edge.ComProc
	AddNewWindowRequested                  edge.ComProc
	RemoveNewWindowRequested               edge.ComProc
	AddDocumentTitleChanged                edge.ComProc
	RemoveDocumentTitleChanged             edge.ComProc
	GetDocumentTitle                       edge.ComProc
	AddHostObjectToScript                  edge.ComProc
	RemoveHostObjectFromScript             edge.ComProc
	OpenDevToolsWindow                     edge.ComProc
	AddContainsFullScreenElementChanged    edge.ComProc
	RemoveContainsFullScreenElementChanged edge.ComProc
	GetContainsFullScreenElement           edge.ComProc
	AddWebResourceRequested                edge.ComProc
	RemoveWebResourceRequested             edge.ComProc
	AddWebResourceRequestedFilter          edge.ComProc
	RemoveWebResourceRequestedFilter       edge.ComProc
	AddWindowCloseRequested                edge.ComProc
	RemoveWindowCloseRequested             edge.ComProc
	AddWebResourceResponseReceived         edge.ComProc
	RemoveWebResourceResponseReceived      edge.ComProc
}

type iCoreWebView2_2 struct {
	vtbl *iCoreWebView2_2Vtbl
}

type navigationCompletedEventArgsVtbl struct {
	iUnknownVtbl
	GetIsSuccess      edge.ComProc
	GetWebErrorStatus edge.ComProc
	GetNavigationID   edge.ComProc
}

type navigationCompletedEventArgs struct {
	vtbl *navigationCompletedEventArgsVtbl
}

type webResourceResponseReceivedEventArgsVtbl struct {
	iUnknownVtbl
	GetRequest  edge.ComProc
	GetResponse edge.ComProc
}

type webResourceResponseReceivedEventArgs struct {
	vtbl *webResourceResponseReceivedEventArgsVtbl
}

type webResourceRequestVtbl struct {
	iUnknownVtbl
	GetURI     edge.ComProc
	PutURI     edge.ComProc
	GetMethod  edge.ComProc
	PutMethod  edge.ComProc
	GetContent edge.ComProc
	PutContent edge.ComProc
	GetHeaders edge.ComProc
}

type webResourceRequest struct {
	vtbl *webResourceRequestVtbl
}

func (r *webResourceRequest) getURI() string {
	var value *uint16
	hr, _, _ := r.vtbl.GetURI.Call(uintptr(unsafe.Pointer(r)), uintptr(unsafe.Pointer(&value)))
	if windows.Handle(hr) != windows.S_OK || value == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value)
}

type webResourceResponseViewVtbl struct {
	iUnknownVtbl
	GetHeaders      edge.ComProc
	GetStatusCode   edge.ComProc
	GetReasonPhrase edge.ComProc
	GetContent      edge.ComProc
}

type webResourceResponseView struct {
	vtbl *webResourceResponseViewVtbl
}

func (r *webResourceResponseView) header(name string) string {
	var headers *httpResponseHeaders
	hr, _, _ := r.vtbl.GetHeaders.Call(uintptr(unsafe.Pointer(r)), uintptr(unsafe.Pointer(&headers)))
	if windows.Handle(hr) != windows.S_OK || headers == nil {
		return ""
	}
	return headers.getHeader(name)
}

type httpResponseHeadersVtbl struct {
	iUnknownVtbl
	AppendHeader edge.ComProc
	Contains     edge.ComProc
	GetHeader    edge.ComProc
	GetHeaders   edge.ComProc
	GetIterator  edge.ComProc
}

type httpResponseHeaders struct {
	vtbl *httpResponseHeadersVtbl
}

func (h *httpResponseHeaders) getHeader(name string) string {
	headerName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	var value *uint16
	hr, _, _ := h.vtbl.GetHeader.Call(uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(headerName)), uintptr(unsafe.Pointer(&value)))
	if windows.Handle(hr) != windows.S_OK || value == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value)
}

type iStreamVtbl struct {
	iUnknownVtbl
	Read  edge.ComProc
	Write edge.ComProc
}

type iStream struct {
	vtbl *iStreamVtbl
}

func (s *iStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var n int
	hr, _, err := s.vtbl.Read.Call(uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)), uintptr(unsafe.Pointer(&n)))
	if err != windows.ERROR_SUCCESS {
		return 0, err
	}
	switch windows.Handle(hr) {
	case windows.S_OK:
		return n, nil
	case windows.S_FALSE:
		return n, io.EOF
	default:
		return 0, syscall.Errno(hr)
	}
}

type webResourceResponseReceivedHandler struct {
	vtbl *responseReceivedHandlerVtbl
	ref  atomic.Uint32
	app  *verifierApp
}

type responseReceivedHandlerVtbl struct {
	QueryInterface edge.ComProc
	AddRef         edge.ComProc
	Release        edge.ComProc
	Invoke         edge.ComProc
}

var responseReceivedHandlerVTable = responseReceivedHandlerVtbl{
	QueryInterface: edge.NewComProc(responseHandlerQueryInterface),
	AddRef:         edge.NewComProc(responseHandlerAddRef),
	Release:        edge.NewComProc(responseHandlerRelease),
	Invoke:         edge.NewComProc(responseHandlerInvoke),
}

func newWebResourceResponseReceivedHandler(app *verifierApp) *webResourceResponseReceivedHandler {
	handler := &webResourceResponseReceivedHandler{vtbl: &responseReceivedHandlerVTable, app: app}
	handler.ref.Store(1)
	return handler
}

func responseHandlerQueryInterface(this, _ uintptr, object uintptr) uintptr {
	if object != 0 {
		*(*uintptr)(unsafe.Pointer(object)) = this
		(*webResourceResponseReceivedHandler)(unsafe.Pointer(this)).ref.Add(1)
	}
	return 0
}

func responseHandlerAddRef(this uintptr) uintptr {
	return uintptr((*webResourceResponseReceivedHandler)(unsafe.Pointer(this)).ref.Add(1))
}

func responseHandlerRelease(this uintptr) uintptr {
	return uintptr((*webResourceResponseReceivedHandler)(unsafe.Pointer(this)).ref.Add(^uint32(0)))
}

func responseHandlerInvoke(this, _ uintptr, args uintptr) uintptr {
	(*webResourceResponseReceivedHandler)(unsafe.Pointer(this)).app.onResponseReceived(args)
	return 0
}

type responseContentHandler struct {
	vtbl        *contentHandlerVtbl
	ref         atomic.Uint32
	app         *verifierApp
	feedURL     string
	contentType string
}

type contentHandlerVtbl struct {
	QueryInterface edge.ComProc
	AddRef         edge.ComProc
	Release        edge.ComProc
	Invoke         edge.ComProc
}

var contentHandlerVTable = contentHandlerVtbl{
	QueryInterface: edge.NewComProc(contentHandlerQueryInterface),
	AddRef:         edge.NewComProc(contentHandlerAddRef),
	Release:        edge.NewComProc(contentHandlerRelease),
	Invoke:         edge.NewComProc(contentHandlerInvoke),
}

func newResponseContentHandler(app *verifierApp, feedURL string, contentType string) *responseContentHandler {
	handler := &responseContentHandler{vtbl: &contentHandlerVTable, app: app, feedURL: feedURL, contentType: contentType}
	handler.ref.Store(1)
	return handler
}

func contentHandlerQueryInterface(this, _ uintptr, object uintptr) uintptr {
	if object != 0 {
		*(*uintptr)(unsafe.Pointer(object)) = this
		(*responseContentHandler)(unsafe.Pointer(this)).ref.Add(1)
	}
	return 0
}

func contentHandlerAddRef(this uintptr) uintptr {
	return uintptr((*responseContentHandler)(unsafe.Pointer(this)).ref.Add(1))
}

func contentHandlerRelease(this uintptr) uintptr {
	return uintptr((*responseContentHandler)(unsafe.Pointer(this)).ref.Add(^uint32(0)))
}

func contentHandlerInvoke(this, errorCode uintptr, streamPtr uintptr) uintptr {
	handler := (*responseContentHandler)(unsafe.Pointer(this))
	if int32(errorCode) < 0 || streamPtr == 0 {
		handler.app.log.Printf("response body unavailable feed=%s error=0x%x", handler.feedURL, errorCode)
		return errorCode
	}
	stream := (*iStream)(unsafe.Pointer(streamPtr))
	body, err := io.ReadAll(stream)
	if err != nil {
		handler.app.log.Printf("response body read failed feed=%s error=%s", handler.feedURL, err)
		return 0
	}
	handler.app.onResponseBody(handler.feedURL, handler.contentType, string(body))
	return 0
}

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func edgeGUID(value string) *guid {
	parsed, err := windows.GUIDFromString(value)
	if err != nil {
		return nil
	}
	return (*guid)(unsafe.Pointer(&parsed))
}
