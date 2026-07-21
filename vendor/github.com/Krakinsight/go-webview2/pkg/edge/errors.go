//go:build windows
// +build windows

package edge

import "fmt"

// ************************************************************************************************
// Error definitions for the webview2 WebAuthn bridge module.
var (
	ErrTooManyArguments = fmt.Errorf("0x%X%X too_many_arguments", "WEBVIEW-EDGE", []byte{0x01})
)
