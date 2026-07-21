package webview2

import (
	"sync"
	"time"
)

// WebAuthnUser represents user information for WebAuthn operations
type WebAuthnUser struct {
	ID          string // User ID (base64url encoded)
	Name        string // User name (email)
	DisplayName string // Display name
}

// WebAuthnOperation describes what is being requested, passed to the approval callback
type WebAuthnOperation struct {
	Type   string       // "create" or "get"
	RPID   string       // Relying Party ID
	RPName string       // Relying Party Name
	User   WebAuthnUser // Empty for "get" operations
}

// WebAuthnBridge provides a JavaScript bridge for WebAuthn functionality.
// Since WebView2's sandbox blocks access to the platform authenticator (Windows Hello/FIDO2),
// this bridge intercepts navigator.credentials calls and routes them through Go handlers.
//
// Operation flow (when CreateHandler/GetHandler are nil):
//  1. OnUserApproval (optional gate): nil or returns false → proceed to Windows Hello.
//     Returns true → abort the operation (user explicitly denied).
//  2. Windows Hello via webauthn.dll.
//  3. If Windows Hello fails and OnWindowsHelloFallback != nil → use internal ECDSA implementation.
//
// When CreateHandler or GetHandler are set, they fully replace the above flow.
type WebAuthnBridge struct {
	// CreateHandler fully replaces the default create flow (Windows Hello + ECDSA fallback).
	// When set, OnUserApproval, OnWindowsHelloFallback and Store are ignored for create operations.
	// Useful to delegate credential creation to an external server, HSM, or custom authenticator.
	CreateHandler func(options WebAuthnCreateOptions) (WebAuthnCredential, error)

	// GetHandler fully replaces the default get flow (Windows Hello + ECDSA fallback).
	// When set, OnUserApproval, OnWindowsHelloFallback and Store are ignored for get operations.
	// Useful to delegate assertion to an external server, HSM, or custom authenticator.
	GetHandler func(options WebAuthnGetOptions) (WebAuthnAssertion, error)

	// OnUserApproval is an optional pre-check gate called before Windows Hello.
	// Ignored when CreateHandler/GetHandler are set.
	// - nil: proceed directly to Windows Hello (no gate)
	// - returns false: proceed to Windows Hello (user approved or no preference)
	// - returns true: abort the operation (user explicitly denied/cancelled)
	OnUserApproval func(op WebAuthnOperation) bool

	// OnWindowsHelloFallback is called when Windows Hello fails.
	// Ignored when CreateHandler/GetHandler are set.
	// - nil: return the Windows Hello error to the caller
	// - non-nil: use internal ECDSA implementation as fallback
	// Return true to use internal ECDSA, false to propagate the Windows Hello error.
	OnWindowsHelloFallback func(op WebAuthnOperation, whErr error) bool

	// Store is the credential storage implementation used by the internal ECDSA fallback.
	// If nil, a default encrypted file-based store will be created automatically.
	// You can inject your own implementation by setting this field before calling
	// EnableWebAuthnBridge or by setting it directly on the returned bridge.
	Store CredentialStore

	webview WebView
	timeout time.Duration
	mu      sync.Mutex
	pending bool // Only one WebAuthn operation at a time
}

// WebAuthnCreateOptions represents the options for creating a new credential
type WebAuthnCreateOptions struct {
	Challenge              string                 `json:"challenge"`
	RP                     RelyingParty           `json:"rp"`
	User                   User                   `json:"user"`
	PubKeyCredParams       []PubKeyCredParam      `json:"pubKeyCredParams"`
	AuthenticatorSelection AuthenticatorSelection `json:"authenticatorSelection,omitempty"`
	ExcludeCredentials     []string               `json:"excludeCredentials,omitempty"`
	Timeout                int                    `json:"timeout,omitempty"`
	Attestation            string                 `json:"attestation,omitempty"`
	Origin                 string                 `json:"origin,omitempty"` // Page origin for clientDataJSON
}

// WebAuthnGetOptions represents the options for getting an assertion
type WebAuthnGetOptions struct {
	Challenge        string   `json:"challenge"`
	RPID             string   `json:"rpId,omitempty"`
	AllowCredentials []string `json:"allowCredentials,omitempty"`
	Timeout          int      `json:"timeout,omitempty"`
	UserVerification string   `json:"userVerification,omitempty"`
	Origin           string   `json:"origin,omitempty"` // Page origin for clientDataJSON
}

// RelyingParty represents the relying party information
type RelyingParty struct {
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
}

// User represents the user information
type User struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// PubKeyCredParam represents a public key credential parameter
type PubKeyCredParam struct {
	Type string `json:"type"`
	Alg  int    `json:"alg"`
}

// AuthenticatorSelection represents authenticator selection criteria
type AuthenticatorSelection struct {
	AuthenticatorAttachment string `json:"authenticatorAttachment,omitempty"`
	RequireResidentKey      bool   `json:"requireResidentKey,omitempty"`
	UserVerification        string `json:"userVerification,omitempty"`
}

// WebAuthnCredential represents a created credential
type WebAuthnCredential struct {
	ID       string             `json:"id"`
	RawID    string             `json:"rawId"`
	Type     string             `json:"type"`
	Response CredentialResponse `json:"response"`
}

// CredentialResponse contains the credential response data
type CredentialResponse struct {
	ClientDataJSON    string `json:"clientDataJSON"`
	AttestationObject string `json:"attestationObject"`
}

// WebAuthnAssertion represents an assertion result
type WebAuthnAssertion struct {
	ID       string            `json:"id"`
	RawID    string            `json:"rawId"`
	Type     string            `json:"type"`
	Response AssertionResponse `json:"response"`
}

// AssertionResponse contains the assertion response data
type AssertionResponse struct {
	ClientDataJSON    string `json:"clientDataJSON"`
	AuthenticatorData string `json:"authenticatorData"`
	Signature         string `json:"signature"`
	UserHandle        string `json:"userHandle,omitempty"`
}
