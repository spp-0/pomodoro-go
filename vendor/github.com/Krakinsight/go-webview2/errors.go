//go:build windows
// +build windows

package webview2

import "fmt"

// ************************************************************************************************
// Error definitions for the webview2 WebAuthn bridge module.
var (
	// ErrWebAuthnDLLNotAvailable is returned when webauthn.dll cannot be loaded on this system.
	ErrWebAuthnDLLNotAvailable = fmt.Errorf("0x%X%X webauthn_dll_not_available", "WEBVIEW-WEBAUTH", []byte{0x01})

	// ErrWindowsHelloNoCredential is returned when Windows Hello has no credential for the requested RP.
	// Callers can use errors.Is to detect this and silently fall back to internal ECDSA.
	ErrWindowsHelloNoCredential = fmt.Errorf("0x%X%X windows_hello_no_credential", "WEBVIEW-WEBAUTH", []byte{0x02})

	// ErrCredentialAttestationNil is returned when WebAuthNAuthenticatorMakeCredential returns a nil attestation.
	ErrCredentialAttestationNil = fmt.Errorf("0x%X%X credential_attestation_nil", "WEBVIEW-WEBAUTH", []byte{0x03})

	// ErrOperationAlreadyInProgress is returned when a WebAuthn operation is already running.
	ErrOperationAlreadyInProgress = fmt.Errorf("0x%X%X operation_already_in_progress", "WEBVIEW-WEBAUTH", []byte{0x04})

	// ErrOperationCancelledByUser is returned when the user approval callback denies the operation.
	ErrOperationCancelledByUser = fmt.Errorf("0x%X%X operation_cancelled_by_user", "WEBVIEW-WEBAUTH", []byte{0x05})

	// ErrNoWindowHandle is returned when the webview window handle cannot be retrieved.
	ErrNoWindowHandle = fmt.Errorf("0x%X%X no_window_handle", "WEBVIEW-WEBAUTH", []byte{0x06})

	// ErrAssertionNil is returned when WebAuthNAuthenticatorGetAssertion returns a nil assertion.
	ErrAssertionNil = fmt.Errorf("0x%X%X assertion_nil", "WEBVIEW-WEBAUTH", []byte{0x07})

	// ErrCredentialNotFound is returned when a credential cannot be located in the store.
	ErrCredentialNotFound = fmt.Errorf("0x%X%X credential_not_found", "WEBVIEW-WEBAUTH", []byte{0x08})

	// ErrNoMatchingCredential is returned when no credential matches the allowCredentials list.
	ErrNoMatchingCredential = fmt.Errorf("0x%X%X no_matching_credential", "WEBVIEW-WEBAUTH", []byte{0x09})

	// ErrAppDataNotFound is returned when the APPDATA directory cannot be determined.
	ErrAppDataNotFound = fmt.Errorf("0x%X%X appdata_not_found", "WEBVIEW-WEBAUTH", []byte{0x0A})

	// ErrCiphertextTooShort is returned when decrypting a credential file that is truncated or corrupt.
	ErrCiphertextTooShort = fmt.Errorf("0x%X%X ciphertext_too_short", "WEBVIEW-WEBAUTH", []byte{0x0B})

	// ErrInvalidPrivateKeyLength is returned when a stored private key has an unexpected byte length.
	ErrInvalidPrivateKeyLength = fmt.Errorf("0x%X%X invalid_private_key_length", "WEBVIEW-WEBAUTH", []byte{0x0C})

	// ErrInvalidUserID is returned when a user ID is invalid.
	ErrInvalidUserID = fmt.Errorf("0x%X%X invalid_user_id", "WEBVIEW-WEBAUTH", []byte{0x0D})

	// ErrEmptyData is returned when an empty byte slice is passed to DPAPI encrypt/decrypt.
	ErrEmptyData = fmt.Errorf("0x%X%X empty_data", "WEBVIEW-WEBAUTH", []byte{0x0E})

	ErrFailedToCreateWebViewWindow = fmt.Errorf("0x%X%X failed_to_create_webview_window", "WEBVIEW-GENERAL", []byte{0x01})
)
