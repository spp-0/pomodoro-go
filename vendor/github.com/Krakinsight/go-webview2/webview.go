//go:build windows
// +build windows

package webview2

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"github.com/Krakinsight/go-webview2/internal/w32"
	"github.com/Krakinsight/go-webview2/pkg/edge"

	"golang.org/x/sys/windows"
)

var (
	windowContext     = map[uintptr]interface{}{}
	windowContextSync sync.RWMutex
)

func getWindowContext(wnd uintptr) interface{} {
	windowContextSync.RLock()
	defer windowContextSync.RUnlock()
	return windowContext[wnd]
}

func setWindowContext(wnd uintptr, data interface{}) {
	windowContextSync.Lock()
	defer windowContextSync.Unlock()
	windowContext[wnd] = data
}

// ************************************************************************************************
// Location represents a 2D position with X and Y coordinates.
// Used for positioning windows on the screen.
//
// Coordinates can be positive or negative:
// - Positive coordinates position from top-left corner of the screen
// - Negative coordinates position from bottom-right corner of the screen
//
// Examples:
//   - Location{X: 0, Y: 0}        -> Top-left corner
//   - Location{X: 100, Y: 100}    -> 100px from left, 100px from top
//   - Location{X: -20, Y: -50}    -> 20px from right, 50px from bottom (taskbar-safe)
//   - Location{X: -500, Y: 50}    -> 500px from right edge, 50px from top
//   - Location{X: 100, Y: -100}   -> 100px from left, 100px from bottom
//
// Note: When using negative Y coordinates, consider the taskbar height (typically 40-50px)
// to avoid window overlap.
type Location struct {
	X int32 // X coordinate (positive: from left, negative: from right)
	Y int32 // Y coordinate (positive: from top, negative: from bottom)
}

type browser interface {
	Embed(hwnd uintptr) bool
	Resize()
	Navigate(url string)
	NavigateToString(htmlContent string)
	Init(script string)
	Eval(script string)
	NotifyParentWindowPositionChanged() error
	Focus()
}

type webview struct {
	hwnd       uintptr
	mainthread uintptr
	browser    browser
	settings   *edge.ICoreWebViewSettings
	autofocus  bool
	maxsz      w32.Point
	minsz      w32.Point
	m          sync.Mutex
	bindings   map[string]interface{}
	dispatchq  []func()
}

type WebViewOptions struct {
	Window unsafe.Pointer
	Debug  bool

	// DataPath specifies the datapath for the WebView2 runtime to use for the
	// browser instance.
	DataPath string

	// AutoFocus will try to keep the WebView2 widget focused when the window
	// is focused.
	AutoFocus bool

	// UserAgent specifies a custom User-Agent string for the WebView2 instance.
	// If empty, the default Edge User-Agent is used.
	UserAgent string

	// WindowOptions customizes the window that is created to embed the
	// WebView2 widget.
	WindowOptions WindowOptions

	// WebAuthn configures the WebAuthn bridge.
	// Set Enabled to true to automatically intercept navigator.credentials calls
	// and route them through Windows Hello / internal ECDSA fallback.
	WebAuthn WebAuthnOptions
}

// ************************************************************************************************
// WebAuthnOptions configures the WebAuthn bridge that is created via WebViewOptions.
//
// Example usage:
//
//	webview2.NewWithOptions(webview2.WebViewOptions{
//	    WebAuthn: webview2.WebAuthnOptions{
//	        Enabled: true,
//	        OnWindowsHelloFallback: func(op webview2.WebAuthnOperation, err error) bool {
//	            return true // use internal ECDSA when Windows Hello is unavailable
//	        },
//	    },
//	})
type WebAuthnOptions struct {
	// Enabled activates the WebAuthn bridge automatically after window creation.
	// When false (default), the bridge is not installed; call EnableWebAuthnBridge() manually.
	Enabled bool

	// CreateHandler fully replaces the default create flow (Windows Hello + ECDSA fallback).
	// When set, OnUserApproval, OnWindowsHelloFallback and Store are ignored for create operations.
	CreateHandler func(options WebAuthnCreateOptions) (WebAuthnCredential, error)

	// GetHandler fully replaces the default get flow (Windows Hello + ECDSA fallback).
	// When set, OnUserApproval, OnWindowsHelloFallback and Store are ignored for get operations.
	GetHandler func(options WebAuthnGetOptions) (WebAuthnAssertion, error)

	// OnUserApproval is an optional gate called before any Windows Hello operation.
	// Return true to abort the operation, false/nil to proceed.
	// Ignored when CreateHandler/GetHandler are set.
	OnUserApproval func(op WebAuthnOperation) bool

	// OnWindowsHelloFallback is called when Windows Hello fails.
	// Return true to use the internal ECDSA fallback, false to propagate the error.
	// Ignored when CreateHandler/GetHandler are set.
	OnWindowsHelloFallback func(op WebAuthnOperation, whErr error) bool

	// Store is the credential storage used by the internal ECDSA fallback.
	// If nil, a default encrypted file store in %APPDATA% is created automatically.
	Store CredentialStore

	// Timeout overrides the default 60-second operation timeout.
	// Zero means use the default.
	Timeout int // seconds
}

// New creates a new webview in a new window.
func New(debug bool) (WebView, error) { return NewWithOptions(WebViewOptions{Debug: debug}) }

// NewWindow creates a new webview using an existing window.
//
// Deprecated: Use NewWithOptions.
func NewWindow(debug bool, window unsafe.Pointer) (WebView, error) {
	return NewWithOptions(WebViewOptions{Debug: debug, Window: window})
}

// NewWithOptions creates a new webview using the provided options.
func NewWithOptions(options WebViewOptions) (WebView, error) {
	w := &webview{}
	w.bindings = map[string]interface{}{}
	w.autofocus = options.AutoFocus

	chromium := edge.NewChromium()
	chromium.MessageCallback = w.msgcb
	chromium.DataPath = options.DataPath
	chromium.SetPermission(edge.CoreWebView2PermissionKindClipboardRead, edge.CoreWebView2PermissionStateAllow)

	w.browser = chromium
	w.mainthread, _, _ = w32.Kernel32GetCurrentThreadID.Call()
	if !w.CreateWithOptions(options.WindowOptions) {
		return nil, ErrFailedToCreateWebViewWindow
	}

	settings, err := chromium.GetSettings()
	if err != nil {
		return nil, fmt.Errorf("failed to get WebView settings: %w", err)
	}
	w.settings = settings
	// disable context menu
	err = w.settings.PutAreDefaultContextMenusEnabled(options.Debug)
	if err != nil {
		return nil, fmt.Errorf("failed to set context menu: %w", err)
	}
	// disable developer tools
	err = w.settings.PutAreDevToolsEnabled(options.Debug)
	if err != nil {
		return nil, fmt.Errorf("failed to set developer tools: %w", err)
	}
	// set custom user agent if provided
	if options.UserAgent != "" {
		err = w.settings.PutUserAgent(options.UserAgent)
		if err != nil {
			return nil, fmt.Errorf("failed to set user agent: %w", err)
		}
	}

	// Activate WebAuthn bridge if requested
	if options.WebAuthn.Enabled {
		bridge := w.EnableWebAuthnBridge()
		bridge.CreateHandler = options.WebAuthn.CreateHandler
		bridge.GetHandler = options.WebAuthn.GetHandler
		bridge.OnUserApproval = options.WebAuthn.OnUserApproval
		bridge.OnWindowsHelloFallback = options.WebAuthn.OnWindowsHelloFallback
		bridge.Store = options.WebAuthn.Store
		if options.WebAuthn.Timeout > 0 {
			bridge.SetTimeout(time.Duration(options.WebAuthn.Timeout) * time.Second)
		}
	}

	return w, nil
}

type rpcMessage struct {
	ID     int               `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

func jsString(v interface{}) string { b, _ := json.Marshal(v); return string(b) }

func (w *webview) msgcb(msg string) {
	d := rpcMessage{}
	if err := json.Unmarshal([]byte(msg), &d); err != nil {
		log.Printf("invalid RPC message: %v", err)
		return
	}

	id := strconv.Itoa(d.ID)
	if res, err := w.callbinding(d); err != nil {
		w.Dispatch(func() {
			w.Eval("window._rpc[" + id + "].reject(" + jsString(err.Error()) + "); window._rpc[" + id + "] = undefined")
		})
	} else if b, err := json.Marshal(res); err != nil {
		w.Dispatch(func() {
			w.Eval("window._rpc[" + id + "].reject(" + jsString(err.Error()) + "); window._rpc[" + id + "] = undefined")
		})
	} else {
		w.Dispatch(func() {
			w.Eval("window._rpc[" + id + "].resolve(" + string(b) + "); window._rpc[" + id + "] = undefined")
		})
	}
}

func (w *webview) callbinding(d rpcMessage) (interface{}, error) {
	w.m.Lock()
	f, ok := w.bindings[d.Method]
	w.m.Unlock()
	if !ok {
		return nil, nil
	}

	v := reflect.ValueOf(f)
	isVariadic := v.Type().IsVariadic()
	numIn := v.Type().NumIn()
	if (isVariadic && len(d.Params) < numIn-1) || (!isVariadic && len(d.Params) != numIn) {
		return nil, errors.New("function arguments mismatch")
	}
	args := []reflect.Value{}
	for i := range d.Params {
		var arg reflect.Value
		if isVariadic && i >= numIn-1 {
			arg = reflect.New(v.Type().In(numIn - 1).Elem())
		} else {
			arg = reflect.New(v.Type().In(i))
		}
		if err := json.Unmarshal(d.Params[i], arg.Interface()); err != nil {
			return nil, err
		}
		args = append(args, arg.Elem())
	}

	errorType := reflect.TypeOf((*error)(nil)).Elem()
	res := v.Call(args)
	switch len(res) {
	case 0:
		// No results from the function, just return nil
		return nil, nil

	case 1:
		// One result may be a value, or an error
		if res[0].Type().Implements(errorType) {
			if res[0].Interface() != nil {
				return nil, res[0].Interface().(error)
			}
			return nil, nil
		}
		return res[0].Interface(), nil

	case 2:
		// Two results: first one is value, second is error
		if !res[1].Type().Implements(errorType) {
			return nil, errors.New("second return value must be an error")
		}
		if res[1].Interface() == nil {
			return res[0].Interface(), nil
		}
		return res[0].Interface(), res[1].Interface().(error)

	default:
		return nil, errors.New("unexpected number of return values")
	}
}

func wndproc(hwnd, msg, wp, lp uintptr) uintptr {
	if w, ok := getWindowContext(hwnd).(*webview); ok {
		switch msg {
		case w32.WMMove, w32.WMMoving:
			_ = w.browser.NotifyParentWindowPositionChanged()
		case w32.WMNCLButtonDown:
			_, _, _ = w32.User32SetFocus.Call(w.hwnd)
			r, _, _ := w32.User32DefWindowProcW.Call(hwnd, msg, wp, lp)
			return r
		case w32.WMSize:
			w.browser.Resize()
		case w32.WMActivate:
			if wp == w32.WAInactive {
				break
			}
			if w.autofocus {
				w.browser.Focus()
			}
		case w32.WMClose:
			_, _, _ = w32.User32DestroyWindow.Call(hwnd)
		case w32.WMDestroy:
			w.Terminate()
		case w32.WMGetMinMaxInfo:
			lpmmi := (*w32.MinMaxInfo)(unsafe.Pointer(lp))
			if w.maxsz.X > 0 && w.maxsz.Y > 0 {
				lpmmi.PtMaxSize = w.maxsz
				lpmmi.PtMaxTrackSize = w.maxsz
			}
			if w.minsz.X > 0 && w.minsz.Y > 0 {
				lpmmi.PtMinTrackSize = w.minsz
			}
		default:
			r, _, _ := w32.User32DefWindowProcW.Call(hwnd, msg, wp, lp)
			return r
		}
		return 0
	}
	r, _, _ := w32.User32DefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

func (w *webview) Create(debug bool, window unsafe.Pointer) bool {
	// This function signature stopped making sense a long time ago.
	// It is but legacy cruft at this point.
	return w.CreateWithOptions(WindowOptions{})
}

func (w *webview) CreateWithOptions(opts WindowOptions) bool {
	var hinstance windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinstance)

	// Set DPI awareness context if specified
	// This affects the entire process and determines how Windows handles DPI scaling.
	// Setting it early ensures consistent DPI behavior for all windows.
	if opts.DpiAwarenessContext != 0 {
		// Try to find the SetProcessDpiAwarenessContext API
		// This API is available on Windows 10 Anniversary Update (1607) and later
		if err := w32.User32SetProcessDpiAwarenessContext.Find(); err == nil {
			// Call SetProcessDpiAwarenessContext with the specified awareness context
			// Returns BOOL (non-zero on success), but we ignore failures since:
			// - Some contexts may not be supported on older Windows versions
			// - DPI awareness may have already been set
			// - This is best-effort configuration
			w32.User32SetProcessDpiAwarenessContext.Call(uintptr(opts.DpiAwarenessContext))
		}
		// If the API is not found (older Windows), we silently continue
		// This ensures backward compatibility with older Windows versions
	}

	var icon uintptr
	if opts.IconId == 0 {
		// load default icon
		icow, _, _ := w32.User32GetSystemMetrics.Call(w32.SystemMetricsCxIcon)
		icoh, _, _ := w32.User32GetSystemMetrics.Call(w32.SystemMetricsCyIcon)
		icon, _, _ = w32.User32LoadImageW.Call(uintptr(hinstance), 32512, icow, icoh, 0)
	} else {
		// load icon from resource
		icon, _, _ = w32.User32LoadImageW.Call(uintptr(hinstance), uintptr(opts.IconId), 1, 0, 0, w32.LR_DEFAULTSIZE|w32.LR_SHARED)
	}

	className, _ := windows.UTF16PtrFromString("webview")
	wc := w32.WndClassExW{
		CbSize:        uint32(unsafe.Sizeof(w32.WndClassExW{})),
		HInstance:     hinstance,
		LpszClassName: className,
		HIcon:         windows.Handle(icon),
		HIconSm:       windows.Handle(icon),
		LpfnWndProc:   windows.NewCallback(wndproc),
	}
	_, _, _ = w32.User32RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	windowName, _ := windows.UTF16PtrFromString(opts.Title)

	// Determine window dimensions
	windowWidth := opts.Width
	if windowWidth == 0 {
		windowWidth = 640
	}
	windowHeight := opts.Height
	if windowHeight == 0 {
		windowHeight = 480
	}

	// Determine window position with priority: explicit Location > Center > default
	var posX, posY uintptr
	if opts.Location != nil {
		// Explicit position specified
		// Support negative coordinates for positioning from screen edges:
		// - Negative X positions from right edge of work area
		// - Negative Y positions from bottom edge of work area (excludes taskbar)
		var workArea w32.Rect
		_, _, _ = w32.User32SystemParametersInfoW.Call(
			w32.SPI_GETWORKAREA,
			0,
			uintptr(unsafe.Pointer(&workArea)),
			0,
		)

		workWidth := workArea.Right - workArea.Left
		workHeight := workArea.Bottom - workArea.Top

		if opts.Location.X < 0 {
			// Position from right edge: workArea.Left + workWidth + X - windowWidth
			posX = uintptr(workArea.Left + workWidth + opts.Location.X - int32(windowWidth))
		} else {
			posX = uintptr(workArea.Left + opts.Location.X)
		}

		if opts.Location.Y < 0 {
			// Position from bottom edge: workArea.Top + workHeight + Y - windowHeight
			posY = uintptr(workArea.Top + workHeight + opts.Location.Y - int32(windowHeight))
		} else {
			posY = uintptr(workArea.Top + opts.Location.Y)
		}
	} else if opts.Center {
		// Calculate centered position
		screenWidth, _, _ := w32.User32GetSystemMetrics.Call(w32.SM_CXSCREEN)
		screenHeight, _, _ := w32.User32GetSystemMetrics.Call(w32.SM_CYSCREEN)
		posX = uintptr((uint(screenWidth) - windowWidth) / 2)
		posY = uintptr((uint(screenHeight) - windowHeight) / 2)
	} else {
		// Use OS default position
		posX = w32.CW_USEDEFAULT
		posY = w32.CW_USEDEFAULT
	}

	// Determine window style
	windowStyle := opts.Style
	if windowStyle == 0 {
		windowStyle = WindowStyleDefault
	}

	w.hwnd, _, _ = w32.User32CreateWindowExW.Call(
		uintptr(opts.ExStyle), // dwExStyle - extended window styles (e.g. WS_EX_TOOLWINDOW)
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		uintptr(windowStyle), // Use specified or default style
		uintptr(posX),
		uintptr(posY),
		uintptr(windowWidth),
		uintptr(windowHeight),
		0,
		0,
		uintptr(hinstance),
		0,
	)
	setWindowContext(w.hwnd, w)

	// WARNING: NEVER keep hidden the window after creating it, or the WebView2 control will fail to initialize and embed properly.
	// This is a quirk of the WebView2 control on Windows - it must be visible at least once to initialize correctly.
	// If you want to start hidden, create the window as visible and then immediately hide it after initialization.
	//if !opts.Hidden {
	w._show()
	//}

	if !w.browser.Embed(w.hwnd) {
		return false
	}
	w.browser.Resize()
	return true
}

func (w *webview) Destroy() {
	_, _, _ = w32.User32PostMessageW.Call(w.hwnd, w32.WMClose, 0, 0)
}

func (w *webview) Run() {
	var msg w32.Msg
	for {
		_, _, _ = w32.User32GetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0,
			0,
			0,
		)
		if msg.Message == w32.WMApp {
			w.m.Lock()
			q := append([]func(){}, w.dispatchq...)
			w.dispatchq = []func(){}
			w.m.Unlock()
			for _, v := range q {
				v()
			}
		} else if msg.Message == w32.WMQuit {
			return
		}
		r, _, _ := w32.User32GetAncestor.Call(uintptr(msg.Hwnd), w32.GARoot)
		r, _, _ = w32.User32IsDialogMessage.Call(r, uintptr(unsafe.Pointer(&msg)))
		if r != 0 {
			continue
		}
		_, _, _ = w32.User32TranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = w32.User32DispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (w *webview) Terminate() {
	_, _, _ = w32.User32PostQuitMessage.Call(0)
}

func (w *webview) Window() unsafe.Pointer {
	return unsafe.Pointer(w.hwnd)
}

func (w *webview) Navigate(url string) {
	w.browser.Navigate(url)
}

func (w *webview) SetHtml(html string) {
	w.browser.NavigateToString(html)
}

func (w *webview) SetTitle(title string) {
	_title, err := windows.UTF16FromString(title)
	if err != nil {
		_title, _ = windows.UTF16FromString("")
	}
	_, _, _ = w32.User32SetWindowTextW.Call(w.hwnd, uintptr(unsafe.Pointer(&_title[0])))
}

func (w *webview) SetSize(width int, height int, hints Hint) {
	index := w32.GWLStyle
	style := w32.GetWindowLong(w.hwnd, index)
	if hints == HintFixed {
		style &^= (w32.WSThickFrame | w32.WSMaximizeBox)
	} else {
		style |= (w32.WSThickFrame | w32.WSMaximizeBox)
	}
	w32.SetWindowLong(w.hwnd, index, style)

	if hints == HintMax {
		w.maxsz.X = int32(width)
		w.maxsz.Y = int32(height)
	} else if hints == HintMin {
		w.minsz.X = int32(width)
		w.minsz.Y = int32(height)
	} else {
		r := w32.Rect{}
		r.Left = 0
		r.Top = 0
		r.Right = int32(width)
		r.Bottom = int32(height)
		_, _, _ = w32.User32AdjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), w32.WSOverlappedWindow, 0)
		_, _, _ = w32.User32SetWindowPos.Call(
			w.hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top),
			w32.SWPNoZOrder|w32.SWPNoActivate|w32.SWPNoMove|w32.SWPFrameChanged)
		w.browser.Resize()
	}
}

func (w *webview) Init(js string) {
	w.browser.Init(js)
}

func (w *webview) Eval(js string) {
	w.browser.Eval(js)
}

func (w *webview) Dispatch(f func()) {
	w.m.Lock()
	w.dispatchq = append(w.dispatchq, f)
	w.m.Unlock()
	_, _, _ = w32.User32PostThreadMessageW.Call(w.mainthread, w32.WMApp, 0, 0)
}

func (w *webview) Bind(name string, f interface{}) error {
	v := reflect.ValueOf(f)
	if v.Kind() != reflect.Func {
		return errors.New("only functions can be bound")
	}
	if n := v.Type().NumOut(); n > 2 {
		return errors.New("function may only return a value or a value+error")
	}
	w.m.Lock()
	w.bindings[name] = f
	w.m.Unlock()

	w.Init("(function() { var name = " + jsString(name) + ";" + `
		var RPC = window._rpc = (window._rpc || {nextSeq: 1});
		window[name] = function() {
		  var seq = RPC.nextSeq++;
		  var promise = new Promise(function(resolve, reject) {
			RPC[seq] = {
			  resolve: resolve,
			  reject: reject,
			};
		  });
		  window.external.invoke(JSON.stringify({
			id: seq,
			method: name,
			params: Array.prototype.slice.call(arguments),
		  }));
		  return promise;
		}
	})()`)

	return nil
}

// ************************************************************************************************
// GetSettings returns the ICoreWebViewSettings interface for configuring WebView2 settings.
// This provides direct access to all WebView2 configuration options including:
// - User-Agent customization
// - Script execution control
// - Context menu behavior
// - DevTools availability
// - Zoom controls
// - And more...
//
// Returns:
//   - *edge.ICoreWebViewSettings: The settings interface for this WebView2 instance
func (w *webview) GetSettings() *edge.ICoreWebViewSettings {
	return w.settings
}

// ************************************************************************************************
// SetAcceleratorKeyCallback sets a callback function to handle keyboard accelerator keys.
// The callback receives virtual key codes and returns true if the key was handled.
//
// This method passes the callback to the underlying Chromium browser instance,
// which registers it with the native WebView2 AcceleratorKeyPressed event handler.
//
// Parameters:
//   - callback: Function that receives virtual key code and returns bool (true = handled)
//
// Virtual key codes are Windows VK_* constants:
// https://learn.microsoft.com/en-us/windows/win32/inputdev/virtual-key-codes
//
// Example usage:
//
//	w := webview2.New(true)
//	w.SetAcceleratorKeyCallback(func(virtualKey uint) bool {
//	    switch virtualKey {
//	    case 0x74: // VK_F5
//	        fmt.Println("Blocked F5 refresh")
//	        return true
//	    case 0x7B: // VK_F12
//	        fmt.Println("Blocked F12 DevTools")
//	        return true
//	    case 0x57: // 'W' key
//	        // Check if Ctrl is pressed separately if needed
//	        fmt.Println("W key pressed")
//	        return false
//	    default:
//	        return false // Allow default handling
//	    }
//	})
//
// Note: The callback is invoked on KEY_DOWN events only, not on key repeat or KEY_UP.
// The underlying implementation automatically filters repeat events using WasKeyDown status.
func (w *webview) SetAcceleratorKeyCallback(callback AcceleratorKeyCallback) {
	chromium, ok := w.browser.(*edge.Chromium)
	if !ok {
		return
	}
	chromium.AcceleratorKeyCallback = callback
}

// ************************************************************************************************
// Hide hides the WebView window without destroying it (SW_HIDE).
// Must be called via Dispatch or from the UI thread.
// The window remains in memory and can be shown again via Show().
//
// Example usage:
//
//	w.Hide()
func (w *webview) Hide() {
	w.Dispatch(func() {
		_, _, _ = w32.User32ShowWindow.Call(w.hwnd, w32.SWHide)
	})
}

// ************************************************************************************************
// Show shows the WebView window and gives it focus (SW_SHOW).
//
// Example usage:
//
//	w.Show()
func (w *webview) Show() {
	w.Dispatch(w._show)
}

// ************************************************************************************************
// ShowUrl shows the WebView window, gives it focus (SW_SHOW), and navigates to the given URL.
// This is a convenience method combining Show() and Navigate() in a single dispatched call,
// ensuring both operations happen atomically on the UI thread.
//
// Parameters:
//   - url: The URL to navigate to (e.g. "https://example.com")
//
// Example usage:
//
//	w.ShowUrl("https://example.com")
func (w *webview) ShowUrl(url string) {
	w.Dispatch(func() {
		w.browser.Navigate(url)
		w._show()
	})
}

// ************************************************************************************************
// IsHidden returns true if the window is hidden, false if visible.
// This method checks the window's visibility state using the Windows IsWindowVisible API.
//
// This method is thread-safe and can be called from any goroutine.
// Unlike Hide() and Show(), it only reads window state without modifying it.
// It directly queries the Windows IsWindowVisible API.
//
// Returns:
//   - bool: true if the window is hidden, false if visible
//
// Example usage:
//
//	if w.IsHidden() {
//	    fmt.Println("Window is hidden")
//	    w.Show()
//	}
func (w *webview) IsHidden() bool {
	ret, _, _ := w32.User32IsWindowVisible.Call(w.hwnd)
	return ret == 0
}

// ************************************************************************************************
// _show shows the WebView window and gives it focus (SW_SHOW).
// Must be called via Dispatch or from the UI thread.
func (w *webview) _show() {
	_, _, _ = w32.User32ShowWindow.Call(w.hwnd, w32.SWShow)
	_, _, _ = w32.User32UpdateWindow.Call(w.hwnd)
	_, _, _ = w32.User32SetFocus.Call(w.hwnd)
}
