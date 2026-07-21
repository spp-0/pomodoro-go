//go:build windows
// +build windows

package webview2

import (
	"fmt"
	"log"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	m_webauthnDLL                         *windows.LazyDLL  = windows.NewLazySystemDLL("webauthn.dll")
	p_WebAuthNGetApiVersionNumber         *windows.LazyProc = m_webauthnDLL.NewProc("WebAuthNGetApiVersionNumber")
	p_WebAuthNAuthenticatorMakeCredential *windows.LazyProc = m_webauthnDLL.NewProc("WebAuthNAuthenticatorMakeCredential")
	p_WebAuthNAuthenticatorGetAssertion   *windows.LazyProc = m_webauthnDLL.NewProc("WebAuthNAuthenticatorGetAssertion")
	p_WebAuthNFreeCredentialAttestation   *windows.LazyProc = m_webauthnDLL.NewProc("WebAuthNFreeCredentialAttestation")
	p_WebAuthNFreeAssertion               *windows.LazyProc = m_webauthnDLL.NewProc("WebAuthNFreeAssertion")
	p_WebAuthNGetErrorName                *windows.LazyProc = m_webauthnDLL.NewProc("WebAuthNGetErrorName")
)

// ************************************************************************************************
// IsWebAuthnDLLAvailable checks if webauthn.dll is available on this system.
func IsWebAuthnDLLAvailable() bool {
	if p_WebAuthNGetApiVersionNumber == nil {
		return false
	}
	return p_WebAuthNGetApiVersionNumber.Find() == nil
}

// ************************************************************************************************
// GetWebAuthnAPIVersion returns the WebAuthn API version if available.
func GetWebAuthnAPIVersion() (uint32, error) {
	if !IsWebAuthnDLLAvailable() {
		return 0, ErrWebAuthnDLLNotAvailable
	}

	version, _, _ := p_WebAuthNGetApiVersionNumber.Call()
	return uint32(version), nil
}

// ************************************************************************************************
// WebAuthnDLLHandler creates handlers that use Windows Hello via webauthn.dll.
// This is a fallback implementation that uses the native Windows WebAuthn API
type WebAuthnDLLHandler struct {
	hwnd uintptr // Window handle for UI
}

// ************************************************************************************************
// NewWebAuthnDLLHandler creates a new handler using webauthn.dll.
// hwnd is the window handle where authentication UI should be displayed.
func NewWebAuthnDLLHandler(hwnd uintptr) (*WebAuthnDLLHandler, error) {
	if !IsWebAuthnDLLAvailable() {
		return nil, ErrWebAuthnDLLNotAvailable
	}

	return &WebAuthnDLLHandler{
		hwnd: hwnd,
	}, nil
}

// Helper function to convert Go string to UTF-16 pointer
func toUTF16Ptr(s string) *uint16 {
	if s == "" {
		return nil
	}
	ptr, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return ptr
}

// ************************************************************************************************
// syscallMakeCredential calls webauthn.dll to create a credential using Windows Hello.
func syscallMakeCredential(hwnd uintptr, options WebAuthnCreateOptions) (WebAuthnCredential, error) {
	if !IsWebAuthnDLLAvailable() {
		return WebAuthnCredential{}, ErrWebAuthnDLLNotAvailable
	}

	// Create RP entity
	rpInfo := webauthnRPEntityInformation{
		dwVersion: 1,
		pwszId:    toUTF16Ptr(options.RP.ID),
		pwszName:  toUTF16Ptr(options.RP.Name),
		pwszIcon:  nil,
	}

	// Decode user ID from base64url
	userID, err := base64URLDecode(options.User.ID)
	if err != nil {
		return WebAuthnCredential{}, fmt.Errorf("%w: invalid user ID with %w", ErrInvalidUserID, err)
	}

	// Create user entity
	userInfo := webauthnUserEntityInformation{
		dwVersion:       1,
		cbId:            uint32(len(userID)),
		pbId:            &userID[0],
		pwszName:        toUTF16Ptr(options.User.Name),
		pwszIcon:        nil,
		pwszDisplayName: toUTF16Ptr(options.User.DisplayName),
	}

	// Build one WEBAUTHN_COSE_CREDENTIAL_PARAMETER per supported algorithm.
	// lAlg is a single int32 — it must NOT be a bitmask.
	credType := toUTF16Ptr("public-key")
	supportedAlgs := []int32{
		WEBAUTHN_COSE_ALGORITHM_ECDSA_P256_WITH_SHA256,
		WEBAUTHN_COSE_ALGORITHM_ECDSA_P384_WITH_SHA384,
		WEBAUTHN_COSE_ALGORITHM_ECDSA_P521_WITH_SHA512,
		WEBAUTHN_COSE_ALGORITHM_RSASSA_PKCS1_V1_5_WITH_SHA256,
		WEBAUTHN_COSE_ALGORITHM_RSASSA_PKCS1_V1_5_WITH_SHA384,
		WEBAUTHN_COSE_ALGORITHM_RSASSA_PKCS1_V1_5_WITH_SHA512,
		WEBAUTHN_COSE_ALGORITHM_RSA_PSS_WITH_SHA256,
		WEBAUTHN_COSE_ALGORITHM_RSA_PSS_WITH_SHA384,
		WEBAUTHN_COSE_ALGORITHM_RSA_PSS_WITH_SHA512,
	}
	credParamsSlice := make([]webauthnCoseCredentialParameter, len(supportedAlgs))
	for i, alg := range supportedAlgs {
		credParamsSlice[i] = webauthnCoseCredentialParameter{
			dwVersion:          1,
			pwszCredentialType: credType,
			lAlg:               alg,
		}
	}
	credParams := webauthnCoseCredentialParameters{
		cCredentialParameters: uint32(len(credParamsSlice)),
		pCredentialParameters: &credParamsSlice[0],
	}

	// Create client data JSON
	origin := options.Origin
	if origin == "" {
		log.Printf("WARNING: No origin provided, using RPID as fallback")
		origin = "https://" + options.RP.ID
	}

	clientDataJSON := fmt.Sprintf(`{"type":"webauthn.create","challenge":"%s","origin":"%s"}`,
		options.Challenge, origin)
	clientDataBytes := []byte(clientDataJSON)

	clientData := webauthnClientData{
		dwVersion:        1,
		cbClientDataJSON: uint32(len(clientDataBytes)),
		pbClientDataJSON: &clientDataBytes[0],
		pwszHashAlgId:    toUTF16Ptr(WEBAUTHN_HASH_ALGORITHM_SHA_256),
	}

	// Resolve authenticator attachment from JS options
	attachment := uint32(WEBAUTHN_AUTHENTICATOR_ATTACHMENT_ANY)
	switch options.AuthenticatorSelection.AuthenticatorAttachment {
	case "platform":
		attachment = WEBAUTHN_AUTHENTICATOR_ATTACHMENT_PLATFORM
	case "cross-platform":
		attachment = WEBAUTHN_AUTHENTICATOR_ATTACHMENT_CROSS_PLATFORM
	}

	// Resolve user verification requirement from JS options
	userVerification := uint32(WEBAUTHN_USER_VERIFICATION_REQUIREMENT_PREFERRED)
	switch options.AuthenticatorSelection.UserVerification {
	case "required":
		userVerification = WEBAUTHN_USER_VERIFICATION_REQUIREMENT_REQUIRED
	case "discouraged":
		userVerification = WEBAUTHN_USER_VERIFICATION_REQUIREMENT_DISCOURAGED
	}

	residentKey := uint32(0)
	if options.AuthenticatorSelection.RequireResidentKey {
		residentKey = 1
	}

	// Build excluded credentials list if provided
	var excludeCredentials []webauthnCredential
	for _, excl := range options.ExcludeCredentials {
		decodedID, decErr := base64URLDecode(excl)
		if decErr != nil {
			continue
		}
		excludeCredentials = append(excludeCredentials, webauthnCredential{
			dwVersion:          1,
			cbId:               uint32(len(decodedID)),
			pbId:               &decodedID[0],
			pwszCredentialType: toUTF16Ptr("public-key"),
		})
	}

	// Use dwVersion capped at 3 (current struct layout)
	apiVersion, _ := GetWebAuthnAPIVersion()
	dwVersion := uint32(3)
	if apiVersion < dwVersion {
		dwVersion = apiVersion
	}
	if dwVersion < 1 {
		dwVersion = 1
	}

	makeCredOptions := webauthnAuthenticatorMakeCredentialOptions{
		dwVersion:                         dwVersion,
		dwTimeoutMilliseconds:             60000,
		dwAuthenticatorAttachment:         attachment,
		bRequireResidentKey:               residentKey,
		dwUserVerificationRequirement:     userVerification,
		dwAttestationConveyancePreference: WEBAUTHN_ATTESTATION_CONVEYANCE_PREFERENCE_ANY,
		dwFlags:                           0,
	}
	if len(excludeCredentials) > 0 {
		makeCredOptions.credentialsToExclude = webauthnCredentials{
			cCredentials: uint32(len(excludeCredentials)),
			pCredentials: &excludeCredentials[0],
		}
	}

	// Call WebAuthNAuthenticatorMakeCredential
	var pCredentialAttestation *webauthnCredentialAttestation
	ret, _, syscallErr := p_WebAuthNAuthenticatorMakeCredential.Call(
		hwnd,
		uintptr(unsafe.Pointer(&rpInfo)),
		uintptr(unsafe.Pointer(&userInfo)),
		uintptr(unsafe.Pointer(&credParams)),
		uintptr(unsafe.Pointer(&clientData)),
		uintptr(unsafe.Pointer(&makeCredOptions)),
		uintptr(unsafe.Pointer(&pCredentialAttestation)),
	)

	if ret != 0 {
		return WebAuthnCredential{}, fmt.Errorf("%w: WebAuthNAuthenticatorMakeCredential HRESULT=0x%08x syscall=%v", ErrWindowsHelloNoCredential, ret, syscallErr)
	}

	if pCredentialAttestation == nil {
		return WebAuthnCredential{}, ErrAssertionNil
	}

	defer p_WebAuthNFreeCredentialAttestation.Call(uintptr(unsafe.Pointer(pCredentialAttestation)))

	// Extract credential ID
	credID := make([]byte, pCredentialAttestation.cbCredentialId)
	for i := uint32(0); i < pCredentialAttestation.cbCredentialId; i++ {
		credID[i] = *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(pCredentialAttestation.pbCredentialId)) + uintptr(i)))
	}

	// Extract authenticator data
	authData := make([]byte, pCredentialAttestation.cbAuthenticatorData)
	for i := uint32(0); i < pCredentialAttestation.cbAuthenticatorData; i++ {
		authData[i] = *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(pCredentialAttestation.pbAuthenticatorData)) + uintptr(i)))
	}

	// Extract attestation object if available
	var attestationObject []byte
	if pCredentialAttestation.cbAttestationObject > 0 {
		attestationObject = make([]byte, pCredentialAttestation.cbAttestationObject)
		for i := uint32(0); i < pCredentialAttestation.cbAttestationObject; i++ {
			attestationObject[i] = *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(pCredentialAttestation.pbAttestationObject)) + uintptr(i)))
		}
	}

	return WebAuthnCredential{
		ID:    base64URLEncode(credID),
		RawID: base64URLEncode(credID),
		Type:  "public-key",
		Response: CredentialResponse{
			ClientDataJSON:    base64URLEncode(clientDataBytes),
			AttestationObject: base64URLEncode(attestationObject),
		},
	}, nil
}

// ************************************************************************************************
// syscallGetAssertion calls webauthn.dll to get an assertion using Windows Hello.
func syscallGetAssertion(hwnd uintptr, options WebAuthnGetOptions) (WebAuthnAssertion, error) {
	if !IsWebAuthnDLLAvailable() {
		return WebAuthnAssertion{}, ErrWebAuthnDLLNotAvailable
	}

	origin := options.Origin
	if origin == "" {
		log.Printf("WARNING: No origin provided, using RPID as fallback")
		origin = "https://" + options.RPID
	}

	clientDataJSON := fmt.Sprintf(`{"type":"webauthn.get","challenge":"%s","origin":"%s"}`,
		options.Challenge, origin)
	clientDataBytes := []byte(clientDataJSON)

	clientData := webauthnClientData{
		dwVersion:        1,
		cbClientDataJSON: uint32(len(clientDataBytes)),
		pbClientDataJSON: &clientDataBytes[0],
		pwszHashAlgId:    toUTF16Ptr(WEBAUTHN_HASH_ALGORITHM_SHA_256),
	}

	// Resolve user verification from options
	userVerification := uint32(WEBAUTHN_USER_VERIFICATION_REQUIREMENT_PREFERRED)
	switch options.UserVerification {
	case "required":
		userVerification = WEBAUTHN_USER_VERIFICATION_REQUIREMENT_REQUIRED
	case "discouraged":
		userVerification = WEBAUTHN_USER_VERIFICATION_REQUIREMENT_DISCOURAGED
	}

	// Use dwVersion capped at 3 (current struct layout)
	apiVersion, _ := GetWebAuthnAPIVersion()
	dwVersion := uint32(3)
	if apiVersion < dwVersion {
		dwVersion = apiVersion
	}
	if dwVersion < 1 {
		dwVersion = 1
	}

	getAssertionOptions := webauthnAuthenticatorGetAssertionOptions{
		dwVersion:                     dwVersion,
		dwTimeoutMilliseconds:         60000,
		dwAuthenticatorAttachment:     WEBAUTHN_AUTHENTICATOR_ATTACHMENT_PLATFORM,
		dwUserVerificationRequirement: userVerification,
		dwFlags:                       0,
	}

	// Set allowed credentials if provided
	var credentials []webauthnCredential
	if len(options.AllowCredentials) > 0 {
		credentials = make([]webauthnCredential, len(options.AllowCredentials))
		for i, credID := range options.AllowCredentials {
			decodedID, err := base64URLDecode(credID)
			if err != nil {
				return WebAuthnAssertion{}, fmt.Errorf("%w: allowCredentials[%d] base64 decode failed: %w", ErrWindowsHelloNoCredential, i, err)
			}
			credentials[i] = webauthnCredential{
				dwVersion:          1,
				cbId:               uint32(len(decodedID)),
				pbId:               &decodedID[0],
				pwszCredentialType: toUTF16Ptr("public-key"),
			}
		}
		getAssertionOptions.credentialsAllowed = webauthnCredentials{
			cCredentials: uint32(len(credentials)),
			pCredentials: &credentials[0],
		}
	}

	// Convert RPID to UTF-16
	rpIDUTF16 := toUTF16Ptr(options.RPID)

	// Call WebAuthNAuthenticatorGetAssertion
	var pAssertion *webauthnAssertion
	ret, _, _ := p_WebAuthNAuthenticatorGetAssertion.Call(
		hwnd,
		uintptr(unsafe.Pointer(rpIDUTF16)),
		uintptr(unsafe.Pointer(&clientData)),
		uintptr(unsafe.Pointer(&getAssertionOptions)),
		uintptr(unsafe.Pointer(&pAssertion)),
	)

	if ret != 0 {
		if ret == WEBAUTHN_NTE_NO_KEY {
			return WebAuthnAssertion{}, ErrWindowsHelloNoCredential
		}
		return WebAuthnAssertion{}, fmt.Errorf("%w: HRESULT=0x%08x", ErrWindowsHelloNoCredential, ret)
	}

	if pAssertion == nil {
		return WebAuthnAssertion{}, ErrAssertionNil
	}

	defer p_WebAuthNFreeAssertion.Call(uintptr(unsafe.Pointer(pAssertion)))

	// Extract credential ID
	credID := make([]byte, pAssertion.credential.cbId)
	for i := uint32(0); i < pAssertion.credential.cbId; i++ {
		credID[i] = *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(pAssertion.credential.pbId)) + uintptr(i)))
	}

	// Extract authenticator data
	authData := make([]byte, pAssertion.cbAuthenticatorData)
	for i := uint32(0); i < pAssertion.cbAuthenticatorData; i++ {
		authData[i] = *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(pAssertion.pbAuthenticatorData)) + uintptr(i)))
	}

	// Extract signature
	signature := make([]byte, pAssertion.cbSignature)
	for i := uint32(0); i < pAssertion.cbSignature; i++ {
		signature[i] = *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(pAssertion.pbSignature)) + uintptr(i)))
	}

	// Extract user ID if available
	var userHandle string
	if pAssertion.cbUserId > 0 {
		userID := make([]byte, pAssertion.cbUserId)
		for i := uint32(0); i < pAssertion.cbUserId; i++ {
			userID[i] = *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(pAssertion.pbUserId)) + uintptr(i)))
		}
		userHandle = base64URLEncode(userID)
	}

	return WebAuthnAssertion{
		ID:    base64URLEncode(credID),
		RawID: base64URLEncode(credID),
		Type:  "public-key",
		Response: AssertionResponse{
			ClientDataJSON:    base64URLEncode(clientDataBytes),
			AuthenticatorData: base64URLEncode(authData),
			Signature:         base64URLEncode(signature),
			UserHandle:        userHandle,
		},
	}, nil
}
