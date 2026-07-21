//go:build windows
// +build windows

package webview2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// EnableWebAuthnBridge enables the WebAuthn bridge on the webview.
// This injects JavaScript that intercepts navigator.credentials calls and routes them through Go handlers.
func (w *webview) EnableWebAuthnBridge() *WebAuthnBridge {
	bridge := &WebAuthnBridge{
		webview: w,
		timeout: 60 * time.Second, // Default 60 second timeout
	}

	// Bind the WebAuthn functions
	w.Bind("__webauthn_create", bridge.handleCreate)
	w.Bind("__webauthn_get", bridge.handleGet)
	w.Bind("__webauthn_isAvailable", bridge.handleIsAvailable)

	// Inject the WebAuthn bridge JavaScript
	w.Init(webauthnBridgeJS)

	return bridge
}

// SetTimeout sets the timeout for WebAuthn operations
func (b *WebAuthnBridge) SetTimeout(timeout time.Duration) {
	b.timeout = timeout
}

// ensureStore initializes the default file-based store if Store is nil
func (b *WebAuthnBridge) ensureStore() error {
	if b.Store == nil {
		store, err := newFileCredentialStore()
		if err != nil {
			return err
		}
		b.Store = store
	}
	return nil
}

// handleCreate handles the credential creation call from JavaScript
func (b *WebAuthnBridge) handleCreate(optionsJSON string) (string, error) {
	// Check if an operation is already pending
	b.mu.Lock()
	if b.pending {
		b.mu.Unlock()
		return "", ErrOperationAlreadyInProgress
	}
	b.pending = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.pending = false
		b.mu.Unlock()
	}()

	var options WebAuthnCreateOptions
	if err := json.Unmarshal([]byte(optionsJSON), &options); err != nil {
		return "", fmt.Errorf("failed to parse create options: %w", err)
	}

	log.Printf("WebAuthn Create request: RP=%s, User=%s", options.RP.Name, options.User.Name)

	// Fast path: custom handler bypasses the entire Windows Hello + fallback flow
	if b.CreateHandler != nil {
		credential, err := b.CreateHandler(options)
		if err != nil {
			return "", err
		}
		result, err := json.Marshal(credential)
		if err != nil {
			return "", fmt.Errorf("failed to marshal credential: %w", err)
		}
		return string(result), nil
	}

	// Create operation description for approval callback
	op := WebAuthnOperation{
		Type:   "create",
		RPID:   options.RP.ID,
		RPName: options.RP.Name,
		User: WebAuthnUser{
			ID:          options.User.ID,
			Name:        options.User.Name,
			DisplayName: options.User.DisplayName,
		},
	}

	// Step 1: OnUserApproval gate — returns true means abort
	if b.OnUserApproval != nil && b.OnUserApproval(op) {
		return "", ErrOperationCancelledByUser
	}

	// Step 2: Windows Hello
	credential, whErr := b.fallbackToWindowsHello(options, WebAuthnGetOptions{})
	if whErr == nil {
		result, err := json.Marshal(credential)
		if err != nil {
			return "", fmt.Errorf("failed to marshal credential: %w", err)
		}
		return string(result), nil
	}

	log.Printf("Windows Hello failed: %v", whErr)

	// Step 3: Fallback to internal ECDSA if allowed
	if b.OnWindowsHelloFallback == nil || !b.OnWindowsHelloFallback(op, whErr) {
		return "", whErr
	}

	log.Printf("Using internal ECDSA fallback")
	credential, err := b.handleCreateInternal(options)
	if err != nil {
		return "", err
	}

	result, err := json.Marshal(credential)
	if err != nil {
		return "", fmt.Errorf("failed to marshal credential: %w", err)
	}

	return string(result), nil
}

// handleGet handles the assertion request from JavaScript
func (b *WebAuthnBridge) handleGet(optionsJSON string) (string, error) {
	// Check if an operation is already pending
	b.mu.Lock()
	if b.pending {
		b.mu.Unlock()
		return "", ErrOperationAlreadyInProgress
	}
	b.pending = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.pending = false
		b.mu.Unlock()
	}()

	var options WebAuthnGetOptions
	if err := json.Unmarshal([]byte(optionsJSON), &options); err != nil {
		return "", fmt.Errorf("failed to parse get options: %w", err)
	}

	log.Printf("WebAuthn Get request: RPID=%s", options.RPID)

	// Fast path: custom handler bypasses the entire Windows Hello + fallback flow
	if b.GetHandler != nil {
		assertion, err := b.GetHandler(options)
		if err != nil {
			return "", err
		}
		result, err := json.Marshal(assertion)
		if err != nil {
			return "", fmt.Errorf("failed to marshal assertion: %w", err)
		}
		return string(result), nil
	}

	// Create operation description for approval callback (user info is empty for "get")
	op := WebAuthnOperation{
		Type:   "get",
		RPID:   options.RPID,
		RPName: options.RPID,   // Use RPID as name for "get" operations
		User:   WebAuthnUser{}, // Empty for "get"
	}

	// Step 1: OnUserApproval gate — returns true means abort
	if b.OnUserApproval != nil && b.OnUserApproval(op) {
		return "", ErrOperationCancelledByUser
	}

	// Step 2: Windows Hello
	assertion, whErr := b.fallbackToWindowsHelloGet(options)
	if whErr == nil {
		result, err := json.Marshal(assertion)
		if err != nil {
			return "", fmt.Errorf("failed to marshal assertion: %w", err)
		}
		return string(result), nil
	}

	log.Printf("Windows Hello failed: %v", whErr)

	// Step 3: Fallback to internal ECDSA if allowed
	if b.OnWindowsHelloFallback == nil || !b.OnWindowsHelloFallback(op, whErr) {
		return "", whErr
	}

	log.Printf("Using internal ECDSA fallback")
	assertion, err := b.handleGetInternal(options)
	if err != nil {
		return "", err
	}

	result, err := json.Marshal(assertion)
	if err != nil {
		return "", fmt.Errorf("failed to marshal assertion: %w", err)
	}

	return string(result), nil
}

// handleIsAvailable handles the availability check from JavaScript
func (b *WebAuthnBridge) handleIsAvailable() bool {
	// WebAuthn is available if either internal store can be initialized or webauthn.dll is available
	return b.Store != nil || IsWebAuthnDLLAvailable()
}

// handleCreateInternal implements credential creation using internal storage
func (b *WebAuthnBridge) handleCreateInternal(options WebAuthnCreateOptions) (WebAuthnCredential, error) {
	if err := b.ensureStore(); err != nil {
		return WebAuthnCredential{}, fmt.Errorf("credential store not available: %w", err)
	}

	// Generate credential ID
	credentialID := make([]byte, 32)
	if _, err := randomBytes(credentialID); err != nil {
		return WebAuthnCredential{}, err
	}
	credIDBase64 := base64URLEncode(credentialID)

	// Generate ECDSA P-256 key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return WebAuthnCredential{}, fmt.Errorf("failed to generate key pair: %w", err)
	}

	// Encode keys for storage
	privateKeyBytes := encodeECDSAPrivateKey(privateKey)
	publicKeyBytes, err := encodeCOSEPublicKey(&privateKey.PublicKey)
	if err != nil {
		return WebAuthnCredential{}, fmt.Errorf("failed to encode public key: %w", err)
	}

	// Use provided origin or fallback to localhost
	origin := options.Origin
	if origin == "" {
		origin = "http://localhost"
	}

	// Create client data JSON with deterministic field ordering
	// WebAuthn spec requires "type" to be first
	clientData := struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
	}{
		Type:      "webauthn.create",
		Challenge: options.Challenge,
		Origin:    origin,
	}
	clientDataJSON, err := json.Marshal(clientData)
	if err != nil {
		return WebAuthnCredential{}, fmt.Errorf("failed to marshal client data: %w", err)
	}
	clientDataBase64 := base64URLEncode(clientDataJSON)

	// Create authenticator data
	// RP ID hash (32 bytes) + flags (1 byte) + counter (4 bytes) = 37 bytes minimum
	rpIDHash := sha256.Sum256([]byte(options.RP.ID))
	authData := make([]byte, 37)
	copy(authData[0:32], rpIDHash[:])
	authData[32] = 0x45 // Flags: UP (0x01) + UV (0x04) + AT (0x40)

	// For attestation, we need to append: AAGUID (16 bytes) + credID length (2 bytes) + credID + public key
	aaguid := make([]byte, 16) // All zeros for this implementation
	authData = append(authData, aaguid...)

	// Credential ID length (big-endian uint16)
	credIDLen := make([]byte, 2)
	binary.BigEndian.PutUint16(credIDLen, uint16(len(credentialID)))
	authData = append(authData, credIDLen...)

	// Credential ID
	authData = append(authData, credentialID...)

	// COSE-encoded public key
	authData = append(authData, publicKeyBytes...)

	// Create attestation object (CBOR-encoded)
	attestationObj, err := createAttestationObject(authData)
	if err != nil {
		return WebAuthnCredential{}, fmt.Errorf("failed to create attestation object: %w", err)
	}
	attestationBase64 := base64URLEncode(attestationObj)

	// Store credential
	cred := StoredCredential{
		ID:         credIDBase64,
		RPID:       options.RP.ID,
		UserID:     options.User.ID,
		UserName:   options.User.Name,
		PrivateKey: privateKeyBytes,
		PublicKey:  publicKeyBytes,
		SignCount:  0,
		CreatedAt:  time.Now(),
	}

	if err := b.Store.Save(cred); err != nil {
		return WebAuthnCredential{}, err
	}

	log.Printf("Created credential with real ECDSA P-256 keypair (ID: %s...)", credIDBase64[:16])

	return WebAuthnCredential{
		ID:    credIDBase64,
		RawID: credIDBase64,
		Type:  "public-key",
		Response: CredentialResponse{
			ClientDataJSON:    clientDataBase64,
			AttestationObject: attestationBase64,
		},
	}, nil
}

// createAttestationObject creates a CBOR-encoded attestation object
func createAttestationObject(authData []byte) ([]byte, error) {
	// CBOR canonical encoding (RFC 7049 §3.9) requires keys sorted by length first, then lexicographically
	// Using fxamacker/cbor with canonical encoding ensures proper key ordering
	// Expected order:
	// 1. "fmt" (3 bytes)
	// 2. "attStmt" (7 bytes)
	// 3. "authData" (8 bytes)

	attestationObj := map[string]interface{}{
		"fmt":      "none",
		"attStmt":  map[string]interface{}{}, // empty map
		"authData": authData,
	}

	// Use canonical CBOR encoding which handles key sorting correctly
	encMode, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		return nil, fmt.Errorf("failed to create CBOR encode mode: %w", err)
	}

	encoded, err := encMode.Marshal(attestationObj)
	if err != nil {
		return nil, fmt.Errorf("failed to encode attestation object: %w", err)
	}

	return encoded, nil
}

// handleGetInternal implements assertion using internal storage
func (b *WebAuthnBridge) handleGetInternal(options WebAuthnGetOptions) (WebAuthnAssertion, error) {
	if err := b.ensureStore(); err != nil {
		return WebAuthnAssertion{}, fmt.Errorf("credential store not available: %w", err)
	}

	// Find matching credential
	var cred StoredCredential
	var found bool

	if len(options.AllowCredentials) > 0 {
		for _, allowedID := range options.AllowCredentials {
			c, err := b.Store.Load(allowedID)
			if err == nil {
				cred = c
				found = true
				break
			}
		}
	} else {
		creds, err := b.Store.LoadAll(options.RPID)
		if err == nil && len(creds) > 0 {
			cred = creds[0]
			found = true
		}
	}

	if !found {
		return WebAuthnAssertion{}, ErrNoMatchingCredential
	}

	// Use provided origin or fallback to localhost
	origin := options.Origin
	if origin == "" {
		origin = "http://localhost"
	}

	// Create client data JSON with deterministic field ordering
	// WebAuthn spec requires "type" to be first
	clientData := struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
	}{
		Type:      "webauthn.get",
		Challenge: options.Challenge,
		Origin:    origin,
	}
	clientDataJSON, err := json.Marshal(clientData)
	if err != nil {
		return WebAuthnAssertion{}, fmt.Errorf("failed to marshal client data: %w", err)
	}
	clientDataBase64 := base64URLEncode(clientDataJSON)

	// Create authenticator data
	rpIDHash := sha256.Sum256([]byte(options.RPID))
	authData := make([]byte, 37)
	copy(authData[0:32], rpIDHash[:])
	authData[32] = 0x05 // Flags: UP (0x01) + UV (0x04)

	// Update sign count
	cred.SignCount++
	binary.BigEndian.PutUint32(authData[33:37], cred.SignCount)

	if err := b.Store.Save(cred); err != nil {
		return WebAuthnAssertion{}, err
	}

	authDataBase64 := base64URLEncode(authData)

	// Decode private key from storage
	privateKey, err := decodeECDSAPrivateKey(cred.PrivateKey)
	if err != nil {
		return WebAuthnAssertion{}, fmt.Errorf("failed to decode private key: %w", err)
	}

	// Create signature over authenticator data + client data hash
	clientDataHash := sha256.Sum256(clientDataJSON)
	signedData := append(authData, clientDataHash[:]...)

	signature, err := signWithECDSA(privateKey, signedData)
	if err != nil {
		return WebAuthnAssertion{}, fmt.Errorf("failed to sign assertion: %w", err)
	}
	signatureBase64 := base64URLEncode(signature)

	log.Printf("Created assertion with real ECDSA signature (cred ID: %s...)", cred.ID[:16])

	return WebAuthnAssertion{
		ID:    cred.ID,
		RawID: cred.ID,
		Type:  "public-key",
		Response: AssertionResponse{
			ClientDataJSON:    clientDataBase64,
			AuthenticatorData: authDataBase64,
			Signature:         signatureBase64,
			UserHandle:        cred.UserID,
		},
	}, nil
}

// fallbackToWindowsHello calls webauthn.dll for credential creation
func (b *WebAuthnBridge) fallbackToWindowsHello(createOpts WebAuthnCreateOptions, _ WebAuthnGetOptions) (WebAuthnCredential, error) {
	if !IsWebAuthnDLLAvailable() {
		return WebAuthnCredential{}, ErrWebAuthnDLLNotAvailable
	}

	// Get window handle from webview
	hwnd := b.getHWND()
	if hwnd == 0 {
		return WebAuthnCredential{}, ErrNoWindowHandle
	}

	// Call Windows Hello via syscall
	return syscallMakeCredential(hwnd, createOpts)
}

// fallbackToWindowsHelloGet calls webauthn.dll for assertion
func (b *WebAuthnBridge) fallbackToWindowsHelloGet(opts WebAuthnGetOptions) (WebAuthnAssertion, error) {
	if !IsWebAuthnDLLAvailable() {
		return WebAuthnAssertion{}, ErrWebAuthnDLLNotAvailable
	}

	// Get window handle from webview
	hwnd := b.getHWND()
	if hwnd == 0 {
		return WebAuthnAssertion{}, ErrNoWindowHandle
	}

	// Call Windows Hello via syscall
	return syscallGetAssertion(hwnd, opts)
}

// getHWND retrieves the window handle from the webview
func (b *WebAuthnBridge) getHWND() uintptr {
	if wv, ok := b.webview.(*webview); ok {
		return wv.hwnd
	}
	return 0
}
