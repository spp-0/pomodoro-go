[![Go](https://github.com/Krakinsight/go-webview2/actions/workflows/go.yml/badge.svg)](https://github.com/Krakinsight/go-webview2/actions/workflows/go.yml) [![Go Report Card](https://goreportcard.com/badge/github.com/Krakinsight/go-webview2)](https://goreportcard.com/report/github.com/Krakinsight/go-webview2) [![Go Reference](https://pkg.go.dev/badge/github.com/Krakinsight/go-webview2.svg)](https://pkg.go.dev/github.com/Krakinsight/go-webview2)

# go-webview2
This package provides an interface for using the Microsoft Edge WebView2 component with Go. It is based on [webview/webview](https://github.com/webview/webview) and provides a compatible API.

Please note that this package only supports Windows, since it provides functionality specific to WebView2. If you wish to use this library for Windows, but use webview/webview for all other operating systems, you could use the [go-webview-selector](https://github.com/Krakinsight/go-webview-selector) package instead. However, you will not be able to use WebView2-specific functionality.

If you wish to build desktop applications in Go using web technologies, please consider [Wails](https://wails.io/). It uses go-webview2 internally on Windows.


If you are using Windows 10+, the WebView2 runtime should already be installed. If not, download it from:

[WebView2 runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/)

## Requirements

- **Go 1.21 or later** - This library uses [`runtime.Pinner`](https://pkg.go.dev/runtime#Pinner) to ensure safe interaction with native Windows COM code
- **Windows 10+** with WebView2 runtime installed

## Basic Usage

```go
package main

import (
    "github.com/Krakinsight/go-webview2"
)

func main() {
    w := webview2.NewWithOptions(webview2.WebViewOptions{
        Debug:     true,
        AutoFocus: true,
        WindowOptions: webview2.WindowOptions{
            Title:  "My App",
            Width:  800,
            Height: 600,
            Center: true,
        },
    })
    
    if w == nil {
        panic("Failed to load webview")
    }
    defer w.Destroy()
    
    w.Navigate("https://example.com")
    w.Run()
}
```

## Features

### Window Positioning

The `Location` struct allows precise window positioning:

```go
// Position from top-left corner
Location: &webview2.Location{X: 100, Y: 100}

// Position from bottom-right corner using negative coordinates
Location: &webview2.Location{X: -500, Y: -1}
```

**Note**: Negative coordinates use Windows work area (excludes taskbar), ensuring windows never overlap the taskbar.

### Custom User-Agent

```go
webview2.NewWithOptions(webview2.WebViewOptions{
    UserAgent: "MyApp/1.0 (CustomBrowser)",
})
```

### Access to WebView2 Settings

```go
w := webview2.NewWithOptions(...)
settings := w.GetSettings()

// Configure zoom controls
settings.PutIsZoomControlEnabled(false)

// Disable browser accelerator keys
settings.PutAreBrowserAcceleratorKeysEnabled(false)
```

### Accelerator Keys

```go
w.SetAcceleratorKeyCallback(func(virtualKey uint) bool {
    switch virtualKey {
    case 0x74: // VK_F5
        fmt.Println("Blocked F5 refresh")
        return true // Block the key
    case 0x7B: // VK_F12  
        fmt.Println("Blocked DevTools")
        return true
    default:
        return false // Allow other keys
    }
})
```

Common virtual key codes:
- `0x74` - F5 (Refresh)
- `0x7B` - F12 (DevTools)
- `0x41-0x5A` - Letters A-Z
- `0x30-0x39` - Numbers 0-9

See [Virtual-Key Codes](https://learn.microsoft.com/en-us/windows/win32/inputdev/virtual-key-codes) for complete reference.

### Window Styles

go-webview2 supports flexible window styling through two separate style parameters:

#### Regular Styles (`Style`)

- `WindowStyleDefault` - Standard resizable window with title bar, system menu, and thick frame
- `WindowStyleFixed` - Non-resizable window with title bar and system menu
- `WindowStyleBorderless` - No borders/title bar (for custom UI)
- `WindowStyleToolWindow` - Tool window with smaller title bar
- `WindowStyleDialog` - Dialog-style window (popup with caption and system menu)

#### Extended Styles (`ExStyle`)

Extended styles control additional window behavior:

- `WindowExStyleDefault` - No extended styles (0)
- `WindowExStyleToolWindow` - Hides window from taskbar (useful for utility windows, popups, overlays)
- `WindowExStyleAppWindow` - Forces window to appear in taskbar
- `WindowExStyleTopMost` - Always on top (stays above all non-topmost windows, even when deactivated)

#### Convenience Constants

For common combinations, use these pre-configured style pairs:

- `WindowStyleBorderlessNoTaskbar` - Borderless popup hidden from taskbar (ideal for overlays, context menus, tooltips)
- `WindowStyleToolWindowNoTaskbar` - Tool window with caption, hidden from taskbar (ideal for floating palettes, property inspectors)

#### Usage Examples

**Standard window (default):**
```go
WindowOptions: webview2.WindowOptions{
    Title:  "My App",
    Width:  800,
    Height: 600,
}
```

**Tool window hidden from taskbar:**
```go
WindowOptions: webview2.WindowOptions{
    Title:   "Tool Palette",
    Width:   300,
    Height:  400,
    Style:   webview2.WindowStyleToolWindow,
    ExStyle: webview2.WindowExStyleToolWindow,
}
```

**Borderless overlay not in taskbar:**
```go
WindowOptions: webview2.WindowOptions{
    Width:   400,
    Height:  300,
    Style:   webview2.WindowStyleBorderless,
    ExStyle: webview2.WindowExStyleToolWindow,
}
```

**Borderless window always on top:**
```go
WindowOptions: webview2.WindowOptions{
    Width:   400,
    Height:  300,
    Style:   webview2.WindowStyleBorderless,
    ExStyle: webview2.WindowExStyleTopMost,
}
```

**Combine multiple extended styles (always on top + hidden from taskbar):**
```go
WindowOptions: webview2.WindowOptions{
    Width:   400,
    Height:  300,
    Style:   webview2.WindowStyleBorderless,
    ExStyle: webview2.WindowExStyleTopMost | webview2.WindowExStyleToolWindow,
}
```

**Note:** To hide a window from the taskbar, you must set `ExStyle: WindowExStyleToolWindow`. The `Style` field alone is not sufficient.

### DPI Awareness

Control how your application handles high-DPI displays to ensure crisp rendering across different display configurations:

```go
w := webview2.NewWithOptions(webview2.WebViewOptions{
    WindowOptions: webview2.WindowOptions{
        Title:                "My DPI-Aware App",
        Width:                800,
        Height:               600,
        DpiAwarenessContext:  webview2.DpiAwarenessContextPerMonitorAwareV2,
    },
})
```

#### Available DPI Awareness Modes:

- `DpiAwarenessContextDefault` - System default (no explicit setting)
- `DpiAwarenessContextUnaware` - Windows handles scaling (may appear blurry)
- `DpiAwarenessContextSystemAware` - Scales to primary monitor DPI
- `DpiAwarenessContextPerMonitorAware` - Adapts to each monitor
- `DpiAwarenessContextPerMonitorAwareV2` - **Recommended** for Windows 10 1703+
- `DpiAwarenessContextUnawareGdiScaled` - Improved unaware mode (Windows 10 1809+)

**Recommendation**: Use `DpiAwarenessContextPerMonitorAwareV2` for modern applications to ensure crisp rendering across all displays. This setting is particularly important for applications that will be used on high-DPI monitors or multi-monitor setups with different DPI settings.

**Note**: The DPI awareness setting affects the entire process and should be set early during window creation. On older Windows versions where the API is unavailable, the setting is silently ignored for backward compatibility.

### WebAuthn Bridge

WebView2's sandbox blocks direct access to the platform authenticator (Windows Hello / FIDO2). The WebAuthn bridge intercepts `navigator.credentials` API calls and routes them through native Go handlers.

**Operation flow:**
1. `OnUserApproval` (optional gate) — return `true` to abort, `false`/`nil` to continue
2. Windows Hello via `webauthn.dll` (full CTAP2 support, including plugin authenticators)
3. If Windows Hello fails and `OnWindowsHelloFallback` returns `true` → internal ECDSA P-256 fallback with encrypted file storage

**Setup via `WebViewOptions` (recommended):**

```go
w := webview2.NewWithOptions(webview2.WebViewOptions{
    WindowOptions: webview2.WindowOptions{
        Title:  "My App",
        Width:  800,
        Height: 600,
        Center: true,
    },
    WebAuthn: webview2.WebAuthnOptions{
        // Enabled activates the bridge automatically
        Enabled: true,

        // Optional: fully replace the create flow (bypasses Windows Hello + ECDSA fallback).
        // Useful to delegate credential creation to an external server or HSM.
        // When set, OnUserApproval, OnWindowsHelloFallback and Store are ignored for create.
        CreateHandler: func(opts webview2.WebAuthnCreateOptions) (webview2.WebAuthnCredential, error) {
            // call your server / HSM here
            return myServer.Register(opts)
        },

        // Optional: fully replace the get flow (bypasses Windows Hello + ECDSA fallback).
        // When set, OnUserApproval, OnWindowsHelloFallback and Store are ignored for get.
        GetHandler: func(opts webview2.WebAuthnGetOptions) (webview2.WebAuthnAssertion, error) {
            return myServer.Authenticate(opts)
        },

        // Optional: gate called before any Windows Hello operation.
        // Return true to abort, false to proceed.
        // Only used when CreateHandler/GetHandler are nil.
        OnUserApproval: func(op webview2.WebAuthnOperation) bool {
            log.Printf("WebAuthn %s for RP=%s user=%s", op.Type, op.RPID, op.User.Name)
            return false // always allow
        },

        // Optional: called when Windows Hello fails.
        // Return true to use internal ECDSA fallback, false to propagate the error.
        // Only used when CreateHandler/GetHandler are nil.
        OnWindowsHelloFallback: func(op webview2.WebAuthnOperation, err error) bool {
            log.Printf("Windows Hello failed (%v), using ECDSA fallback", err)
            return true
        },

        // Optional: custom credential store.
        // nil = default AES-GCM encrypted file in %APPDATA%\go-webview2\
        Store: nil,

        // Optional: operation timeout in seconds (0 = default 60s)
        Timeout: 120,
    },
})
```

**Manual setup (advanced):**

```go
w := webview2.NewWithOptions(...)
bridge := w.EnableWebAuthnBridge()
bridge.OnUserApproval = func(op webview2.WebAuthnOperation) bool { return false }
bridge.OnWindowsHelloFallback = func(op webview2.WebAuthnOperation, err error) bool { return true }
bridge.Store = webview2.NewInMemoryCredentialStore() // or your own implementation
bridge.SetTimeout(90 * time.Second)
```

**Error handling:**

All errors are typed and can be matched with `errors.Is`:

```go
import "errors"

// Check specific error conditions
if errors.Is(err, webview2.ErrWindowsHelloNoCredential) {
    // No Windows Hello credential for this RP
}
if errors.Is(err, webview2.ErrOperationCancelledByUser) {
    // User denied the operation
}
if errors.Is(err, webview2.ErrNoMatchingCredential) {
    // No credential in the store matches allowCredentials
}
if errors.Is(err, webview2.ErrOperationAlreadyInProgress) {
    // Another WebAuthn operation is already running
}
```

**Full error reference:**

| Error | Description |
|-------|-------------|
| `ErrWebAuthnDLLNotAvailable` | `webauthn.dll` cannot be loaded on this system |
| `ErrWindowsHelloNoCredential` | Windows Hello has no credential for the requested RP |
| `ErrCredentialAttestationNil` | `WebAuthNAuthenticatorMakeCredential` returned a nil attestation |
| `ErrOperationAlreadyInProgress` | A WebAuthn operation is already running |
| `ErrOperationCancelledByUser` | The user approval callback denied the operation |
| `ErrNoWindowHandle` | The webview window handle cannot be retrieved |
| `ErrAssertionNil` | `WebAuthNAuthenticatorGetAssertion` returned a nil assertion |
| `ErrCredentialNotFound` | A credential cannot be located in the store |
| `ErrNoMatchingCredential` | No credential matches the `allowCredentials` list |
| `ErrAppDataNotFound` | The `APPDATA` directory cannot be determined |
| `ErrCiphertextTooShort` | Credential file is truncated or corrupt |
| `ErrInvalidPrivateKeyLength` | Stored private key has an unexpected byte length |
| `ErrInvalidUserID` | User ID is invalid |
| `ErrEmptyData` | An empty byte slice was passed to DPAPI encrypt/decrypt |

**Credential Storage:**

The bridge supports pluggable credential storage via the `CredentialStore` interface:

```go
type CredentialStore interface {
    Save(credential StoredCredential) error
    Load(credentialID string) (StoredCredential, error)
    LoadAll(rpID string) ([]StoredCredential, error)
    Delete(credentialID string) error
}
```

Available implementations:
- `NewInMemoryCredentialStore()` — for testing (no persistence, no encryption)
- Default file store (used when `Store: nil`) — AES-GCM encrypted, key derived from Windows user SID, stored in `%APPDATA%\go-webview2\webauthn_credentials.enc`
- Custom implementation — inject via `bridge.Store` or `WebAuthnOptions.Store`

**Windows Hello / CTAP plugins:**

```go
if webview2.IsWebAuthnDLLAvailable() {
    version, _ := webview2.GetWebAuthnAPIVersion()
    log.Printf("Windows Hello available (API version: %d)", version)
}
```

Windows Hello plugin authenticators (1Password, Bitwarden, etc.) are automatically supported — `webauthn.dll` orchestrates authenticator selection internally. No extra configuration needed.

**Supported algorithms:** ES256, ES384, ES512, RS256, RS384, RS512, PS256, PS384, PS512.

**JavaScript API — unchanged:**

The bridge transparently intercepts the standard WebAuthn JavaScript API:

```javascript
// Register a new credential
const credential = await navigator.credentials.create({
    publicKey: {
        challenge: new Uint8Array(32),
        rp: { name: "My App", id: window.location.hostname },
        user: {
            id: new Uint8Array(16),
            name: "user@example.com",
            displayName: "User"
        },
        pubKeyCredParams: [
            { type: "public-key", alg: -7 },   // ES256
            { type: "public-key", alg: -257 }   // RS256
        ],
        authenticatorSelection: { userVerification: "required" }
    }
});

// Authenticate with a credential
const assertion = await navigator.credentials.get({
    publicKey: {
        challenge: new Uint8Array(32),
        rpId: window.location.hostname,   // ← always set rpId explicitly
        allowCredentials: [{
            type: "public-key",
            id: credentialIdBytes         // Uint8Array of raw credential ID bytes
        }],
        userVerification: "required"
    }
});
```

> **Important:** always pass `rpId` explicitly in `navigator.credentials.get()`. If omitted, the bridge defaults to `window.location.hostname` but Windows Hello may reject it if it differs from the enrolled origin.

**Key Features:**
- Bypasses WebView2 sandbox limitations
- Full Windows Hello / CTAP2 integration including plugin authenticators
- Internal ECDSA P-256 fallback for environments without Windows Hello
- Thread-safe (one operation at a time)
- Configurable timeouts
- Pluggable credential storage
- All errors typed and matchable with `errors.Is`

### Window Close from JavaScript

You cannot create a binding named `close()` because it conflicts with the built-in `window.close()` function. However, you can easily add a `closewebview()` function in JavaScript by binding it to `w.Destroy()`:

```go
w := webview2.NewWithOptions(webview2.WebViewOptions{
    Debug:     true,
    WindowOptions: webview2.WindowOptions{
        Title:  "My App",
        Width:  800,
        Height: 600,
    },
})

// Bind close function
w.Bind("closewebview", func() {
    w.Destroy()
})
```

Then in JavaScript:

```javascript
// Simple close button
<button onclick="closewebview()">Close Window</button>

// With confirmation
<button onclick="if(confirm('Close?')) closewebview()">Close</button>
```

### Hide / Show Window

Control window visibility without destroying the webview:

```go
// Hide the window (must be called via Dispatch or from the UI thread)
w.Hide()

// Show the window and give it focus
w.Show()

// Show the window, give it focus, and navigate to a URL (atomic operation)
w.ShowUrl("https://example.com")

// Check if the window is currently hidden
if w.IsHidden() {
    fmt.Println("Window is hidden")
    w.Show()
}
```

[`Hide()`](webview.go:696), [`Show()`](webview.go:708), and [`ShowUrl()`](webview.go:712) methods dispatch internally to the UI thread. [`ShowUrl()`](webview.go:712) is a convenience method combining [`Show()`](webview.go:708) and [`Navigate()`](webview.go:534) in a single atomic call. [`IsHidden()`](webview.go:736) checks the window's visibility state using the Windows `IsWindowVisible` API and returns `true` if the window is hidden.

### Hidden Window at Startup

Create a window that is not shown until you explicitly call `Show()`:

```go
w := webview2.NewWithOptions(webview2.WebViewOptions{
    WindowOptions: webview2.WindowOptions{
        Title:  "My App",
        Width:  800,
        Height: 600,
        Hidden: true, // Window created but not shown
    },
})

// ... setup, load content, bind functions ...

// Show the window when ready
w.Show()
```

## Demos

### Available Demos

**Basic centered window:**
```
go run ./cmd/demo-basic
```

**Positioned window with custom style:**
```
go run ./cmd/demo-positioned
```

**Borderless window:**
```
go run ./cmd/demo-borderless
```

**Tool window (top-right):**
```
go run ./cmd/demo-toolwindow
```

**Bottom-right positioned:**
```
go run ./cmd/demo-bottomright
```

**DPI awareness demonstration:**
```
go run ./cmd/demo-dpi-aware
```

**Accelerator keys (F5/F12 blocking):**
```
go run ./cmd/demo-accelerator-keys
```

**Window close from JavaScript:**
```
go run ./cmd/demo-close
```

**WebAuthn bridge — self-contained HTML demo:**
```
go run ./cmd/demo-webauthn_1
```
Displays an embedded HTML page with register/authenticate buttons. Uses Windows Hello with an optional approval dialog and ECDSA fallback.

**WebAuthn bridge — real-world site (webauthn.io):**
```
go run ./cmd/demo-webauthn_2
```
Navigates to `https://webauthn.io/` with the WebAuthn bridge enabled so the site can use the platform authenticator through the Go bridge.

This will use go-winloader to load an embedded copy of WebView2Loader.dll. If you want, you can also provide a newer version of WebView2Loader.dll in the DLL search path and it should be picked up instead. It can be acquired from the WebView2 SDK (which is permissively licensed.)
