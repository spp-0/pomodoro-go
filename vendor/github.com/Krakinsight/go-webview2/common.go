package webview2

import (
	"unsafe"

	"github.com/Krakinsight/go-webview2/pkg/edge"
)

// This is copied from webview/webview.
// The documentation is included for convenience.

// Hint is used to configure window sizing and resizing behavior.
type Hint int

const (
	// HintNone specifies that width and height are default size
	HintNone Hint = iota

	// HintFixed specifies that window size can not be changed by a user
	HintFixed

	// HintMin specifies that width and height are minimum bounds
	HintMin

	// HintMax specifies that width and height are maximum bounds
	HintMax
)

// WindowStyle defines the visual style and behavior of a window.
type WindowStyle uint32

const (
	// WindowStyleDefault creates a standard overlapped window with title bar,
	// system menu, and thick frame (resizable).
	WindowStyleDefault WindowStyle = 0xCF0000 // WS_OVERLAPPEDWINDOW

	// WindowStyleBorderless creates a window without any borders or decorations.
	// Useful for custom-styled windows or splash screens.
	// Note: This window will appear in the taskbar unless you also set ExStyle to WindowExStyleToolWindow.
	WindowStyleBorderless WindowStyle = 0x80000000 // WS_POPUP

	// WindowStyleToolWindow creates a tool window with a smaller title bar.
	// Must be combined with WindowExStyleToolWindow to hide from taskbar.
	WindowStyleToolWindow WindowStyle = 0x00C80000 // WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU

	// WindowStyleFixed creates a non-resizable window with title bar and system menu.
	WindowStyleFixed WindowStyle = 0x00C80000 // WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU

	// WindowStyleDialog creates a dialog-style window.
	WindowStyleDialog WindowStyle = 0x80C80000 // WS_POPUP | WS_CAPTION | WS_SYSMENU
)

// ************************************************************************************************
// WindowExStyle defines extended window styles used with dwExStyle parameter of CreateWindowExW.
// These styles control additional window behavior such as taskbar visibility, layering, and special effects.
type WindowExStyle uint32

const (
	// WindowExStyleDefault uses no extended styles (0).
	WindowExStyleDefault WindowExStyle = 0

	// WindowExStyleToolWindow creates a window not shown in the taskbar.
	// Useful for floating toolboxes, popups, and utility windows.
	// Corresponds to WS_EX_TOOLWINDOW = 0x00000080
	WindowExStyleToolWindow WindowExStyle = 0x00000080

	// WindowExStyleAppWindow forces the window to be shown in the taskbar.
	// This overrides the default behavior where child windows are hidden.
	// Corresponds to WS_EX_APPWINDOW = 0x00040000
	WindowExStyleAppWindow WindowExStyle = 0x00040000

	// WindowExStyleTopMost creates a window that should be placed above all non-topmost windows
	// and stay above them even when the window is deactivated (always on top).
	// Corresponds to WS_EX_TOPMOST = 0x00000008
	WindowExStyleTopMost WindowExStyle = 0x00000008
)

// ************************************************************************************************
// WindowStyleCombination provides a convenient way to specify both regular and extended window styles.
// Use this when you need a specific combination of Style and ExStyle.
type WindowStyleCombination struct {
	Style   WindowStyle
	ExStyle WindowExStyle
}

var (
	// WindowStyleBorderlessNoTaskbar creates a borderless popup window hidden from taskbar.
	// Use this for overlays, context menus, tooltips, or fullscreen borderless windows.
	WindowStyleBorderlessNoTaskbar = WindowStyleCombination{
		Style:   WindowStyleBorderless,
		ExStyle: WindowExStyleToolWindow,
	}

	// WindowStyleToolWindowNoTaskbar creates a tool window with caption, hidden from taskbar.
	// Use this for floating tool palettes, property inspectors, or auxiliary windows.
	WindowStyleToolWindowNoTaskbar = WindowStyleCombination{
		Style:   WindowStyleToolWindow,
		ExStyle: WindowExStyleToolWindow,
	}
)

// ************************************************************************************************
// DpiAwarenessContext defines how the application handles DPI scaling on Windows.
// Different modes determine how Windows scales the application's windows and content
// on high-DPI displays. Setting DPI awareness ensures crisp rendering across different
// display configurations.
//
// References:
//   - https://learn.microsoft.com/en-us/windows/win32/hidpi/high-dpi-desktop-application-development-on-windows
//   - https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setprocessdpiawarenesscontext
type DpiAwarenessContext int

const (
	// DpiAwarenessContextDefault uses the system default DPI awareness.
	// This is the default behavior if no DPI awareness is explicitly set.
	// When set to 0 (default value), no DPI awareness configuration is applied.
	DpiAwarenessContextDefault DpiAwarenessContext = 0

	// DpiAwarenessContextUnaware means the application is DPI unaware.
	// Windows will bitmap stretch the window on high-DPI displays, which may appear blurry.
	// Value: -1 (DPI_AWARENESS_CONTEXT_UNAWARE)
	DpiAwarenessContextUnaware DpiAwarenessContext = -1

	// DpiAwarenessContextSystemAware means the application is system DPI aware.
	// It scales to match the DPI of the primary display at application startup.
	// Does not adapt when moved to monitors with different DPI settings.
	// Value: -2 (DPI_AWARENESS_CONTEXT_SYSTEM_AWARE)
	DpiAwarenessContextSystemAware DpiAwarenessContext = -2

	// DpiAwarenessContextPerMonitorAware means the application is per-monitor DPI aware.
	// It checks the DPI when created and adjusts scale factors when DPI changes.
	// Requires Windows 10 Anniversary Update (1607) or later.
	// Value: -3 (DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE)
	DpiAwarenessContextPerMonitorAware DpiAwarenessContext = -3

	// DpiAwarenessContextPerMonitorAwareV2 provides enhanced per-monitor DPI awareness.
	// Similar to V1 but with improved support for mixed-mode DPI scaling and child window DPI scaling.
	// Recommended for modern Windows 10+ applications.
	// Requires Windows 10 Creators Update (1703) or later.
	// Value: -4 (DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)
	DpiAwarenessContextPerMonitorAwareV2 DpiAwarenessContext = -4

	// DpiAwarenessContextUnawareGdiScaled is DPI unaware with improved GDI scaling.
	// Better than basic unaware mode with less blurry text rendering.
	// Requires Windows 10 October 2018 Update (1809) or later.
	// Value: -5 (DPI_AWARENESS_CONTEXT_UNAWARE_GDISCALED)
	DpiAwarenessContextUnawareGdiScaled DpiAwarenessContext = -5
)

// ************************************************************************************************
// AcceleratorKeyCallback is called when a keyboard accelerator key is pressed.
// It receives the virtual key code and returns true if the event was handled.
//
// Virtual key codes are defined by Windows:
// https://learn.microsoft.com/en-us/windows/win32/inputdev/virtual-key-codes
//
// Common examples:
//   - VK_F5 (0x74) for F5
//   - VK_F12 (0x7B) for F12
//   - 'A' (0x41) for the A key
//
// The callback is invoked on KEY_DOWN events only (not on KEY_UP or repeat).
// Return true to mark the key as handled and prevent default browser behavior.
// Return false to allow the browser to handle the key normally.
//
// Example usage:
//
//	w.SetAcceleratorKeyCallback(func(virtualKey uint) bool {
//	    if virtualKey == 0x74 { // VK_F5
//	        fmt.Println("F5 pressed - refresh blocked")
//	        return true // Block default refresh
//	    }
//	    if virtualKey == 0x7B { // VK_F12
//	        fmt.Println("F12 pressed - DevTools blocked")
//	        return true // Block DevTools
//	    }
//	    return false // Allow other keys
//	})
type AcceleratorKeyCallback func(virtualKey uint) bool

type WindowOptions struct {
	Title  string
	Width  uint
	Height uint
	IconId uint

	// Location specifies the top-left position of the window.
	// If nil, uses Center behavior if Center is true, otherwise uses OS default.
	// Use the Location struct: &Location{X: 100, Y: 100}
	Location *Location

	// Center centers the window on the screen.
	// Ignored if Location is specified.
	Center bool

	// Style specifies the window style using Windows style constants.
	// Use WindowStyleDefault if not specified (0 value).
	Style WindowStyle

	// ExStyle specifies extended window styles (dwExStyle parameter).
	// Use WindowExStyleDefault (0) for no extended styles.
	// Common usage: WindowExStyleToolWindow to hide window from taskbar.
	ExStyle WindowExStyle

	// DpiAwarenessContext sets the DPI awareness for the process.
	// Use DpiAwarenessContextDefault (0) to skip setting DPI awareness.
	// Recommended: DpiAwarenessContextPerMonitorAwareV2 for modern Windows 10+ apps.
	//
	// Example:
	//   DpiAwarenessContext: webview2.DpiAwarenessContextPerMonitorAwareV2
	//
	// Note: This setting affects the entire process and should be set early.
	// On older Windows versions where the API is unavailable, this setting is silently ignored.
	DpiAwarenessContext DpiAwarenessContext
}

// WebView is the interface for the webview.
type WebView interface {

	// Run runs the main loop until it's terminated. After this function exits -
	// you must destroy the webview.
	Run()

	// Terminate stops the main loop. It is safe to call this function from
	// a background thread.
	Terminate()

	// Dispatch posts a function to be executed on the main thread. You normally
	// do not need to call this function, unless you want to tweak the native
	// window.
	Dispatch(f func())

	// Destroy destroys a webview and closes the native window.
	Destroy()

	// Hide hides the window without destroying it (SW_HIDE).
	Hide()

	// Show shows the window and gives it focus (SW_SHOW).
	Show()

	// ShowUrl opens the specified URL in the default web browser.
	ShowUrl(url string)

	// IsHidden returns true if the window is hidden, false if visible.
	// This method checks the window's visibility state using the Windows IsWindowVisible API.
	IsHidden() bool

	// Window returns a native window handle pointer. When using GTK backend the
	// pointer is GtkWindow pointer, when using Cocoa backend the pointer is
	// NSWindow pointer, when using Win32 backend the pointer is HWND pointer.
	Window() unsafe.Pointer

	// SetTitle updates the title of the native window. Must be called from the UI
	// thread.
	SetTitle(title string)

	// SetSize updates native window size. See Hint constants.
	SetSize(w int, h int, hint Hint)

	// Navigate navigates webview to the given URL. URL may be a data URI, i.e.
	// "data:text/text,<html>...</html>". It is often ok not to url-encode it
	// properly, webview will re-encode it for you.
	Navigate(url string)

	// SetHtml sets the webview HTML directly.
	// The origin of the page is `about:blank`.
	SetHtml(html string)

	// Init injects JavaScript code at the initialization of the new page. Every
	// time the webview will open a the new page - this initialization code will
	// be executed. It is guaranteed that code is executed before window.onload.
	Init(js string)

	// Eval evaluates arbitrary JavaScript code. Evaluation happens asynchronously,
	// also the result of the expression is ignored. Use RPC bindings if you want
	// to receive notifications about the results of the evaluation.
	Eval(js string)

	// Bind binds a callback function so that it will appear under the given name
	// as a global JavaScript function. Internally it uses webview_init().
	// Callback receives a request string and a user-provided argument pointer.
	// Request string is a JSON array of all the arguments passed to the
	// JavaScript function.
	//
	// f must be a function
	// f must return either value and error or just error
	Bind(name string, f interface{}) error

	// GetSettings returns the ICoreWebViewSettings interface for configuring WebView2 settings.
	// This provides direct access to all WebView2 configuration options including:
	// - User-Agent customization
	// - Script execution control
	// - Context menu behavior
	// - DevTools availability
	// - Zoom controls
	// - And more...
	GetSettings() *edge.ICoreWebViewSettings

	// SetAcceleratorKeyCallback sets a callback for handling keyboard accelerator keys.
	// The callback receives the virtual key code and returns true if handled.
	//
	// This allows intercepting and handling keyboard shortcuts before the browser
	// processes them. Common use cases include:
	// - Blocking F5 (refresh)
	// - Blocking F12 (DevTools)
	// - Implementing custom keyboard shortcuts
	// - Overriding default browser keyboard behavior
	//
	// The callback is invoked only on KEY_DOWN events (not KEY_UP or repeat).
	// Virtual key codes follow Windows VK_* constants:
	// https://learn.microsoft.com/en-us/windows/win32/inputdev/virtual-key-codes
	//
	// Example:
	//   w.SetAcceleratorKeyCallback(func(vk uint) bool {
	//       if vk == 0x74 { return true } // Block F5
	//       return false
	//   })
	SetAcceleratorKeyCallback(callback AcceleratorKeyCallback)

	// EnableWebAuthnBridge enables the WebAuthn bridge for handling Web Authentication API calls.
	// Since WebView2's sandbox blocks access to the platform authenticator (Windows Hello/FIDO2),
	// this bridge intercepts navigator.credentials calls and routes them through Go handlers.
	//
	// Returns a *WebAuthnBridge that can be used to set custom handlers for credential
	// creation and assertion operations.
	//
	// Example:
	//   bridge := w.EnableWebAuthnBridge()
	//   bridge.SetCreateHandler(func(opts WebAuthnCreateOptions) (WebAuthnCredential, error) {
	//       // Handle credential creation
	//       return credential, nil
	//   })
	EnableWebAuthnBridge() *WebAuthnBridge
}
