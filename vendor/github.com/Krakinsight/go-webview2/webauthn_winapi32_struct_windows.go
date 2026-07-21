package webview2

import "golang.org/x/sys/windows"

// webauthnRPEntityInformation represents WEBAUTHN_RP_ENTITY_INFORMATION
type webauthnRPEntityInformation struct {
	dwVersion uint32
	pwszId    *uint16
	pwszName  *uint16
	pwszIcon  *uint16
}

// webauthnUserEntityInformation represents WEBAUTHN_USER_ENTITY_INFORMATION
type webauthnUserEntityInformation struct {
	dwVersion       uint32
	cbId            uint32
	pbId            *byte
	pwszName        *uint16
	pwszIcon        *uint16
	pwszDisplayName *uint16
}

// webauthnCoseCredentialParameter represents WEBAUTHN_COSE_CREDENTIAL_PARAMETER
type webauthnCoseCredentialParameter struct {
	dwVersion          uint32
	pwszCredentialType *uint16
	lAlg               int32
}

// webauthnCoseCredentialParameters represents WEBAUTHN_COSE_CREDENTIAL_PARAMETERS
type webauthnCoseCredentialParameters struct {
	cCredentialParameters uint32
	pCredentialParameters *webauthnCoseCredentialParameter
}

// webauthnClientData represents WEBAUTHN_CLIENT_DATA
type webauthnClientData struct {
	dwVersion        uint32
	cbClientDataJSON uint32
	pbClientDataJSON *byte
	pwszHashAlgId    *uint16
}

// webauthnCredential represents WEBAUTHN_CREDENTIAL
type webauthnCredential struct {
	dwVersion          uint32
	cbId               uint32
	pbId               *byte
	pwszCredentialType *uint16
}

// webauthnCredentials represents WEBAUTHN_CREDENTIALS
type webauthnCredentials struct {
	cCredentials uint32
	pCredentials *webauthnCredential
}

// webauthnExtensions represents WEBAUTHN_EXTENSIONS with correct x64 layout
// In webauthn.h: { DWORD cExtensions; PWEBAUTHN_EXTENSION pExtensions; }
// On x64: 4 bytes count + 4 bytes padding + 8 bytes pointer = 16 bytes total
type webauthnExtensions struct {
	cExtensions uint32
	_           uint32  // alignment padding
	pExtensions uintptr // pointer to extensions array (NULL = no extensions)
}

// webauthnAuthenticatorMakeCredentialOptions represents WEBAUTHN_AUTHENTICATOR_MAKE_CREDENTIAL_OPTIONS
type webauthnAuthenticatorMakeCredentialOptions struct {
	dwVersion                         uint32
	dwTimeoutMilliseconds             uint32
	credentialsToExclude              webauthnCredentials
	extensions                        webauthnExtensions
	dwAuthenticatorAttachment         uint32
	bRequireResidentKey               uint32
	dwUserVerificationRequirement     uint32
	dwAttestationConveyancePreference uint32
	dwFlags                           uint32
	pCancellationId                   *windows.GUID
	pExcludeCredentialList            *webauthnCredentials
}

// webauthnAuthenticatorGetAssertionOptions represents WEBAUTHN_AUTHENTICATOR_GET_ASSERTION_OPTIONS
type webauthnAuthenticatorGetAssertionOptions struct {
	dwVersion                     uint32
	dwTimeoutMilliseconds         uint32
	credentialsAllowed            webauthnCredentials
	extensions                    webauthnExtensions
	dwAuthenticatorAttachment     uint32
	dwUserVerificationRequirement uint32
	dwFlags                       uint32
	pwszU2fAppId                  *uint16
	pbU2fAppId                    *uint32
	pCancellationId               *windows.GUID
	pAllowCredentialList          *webauthnCredentials
}

// webauthnCredentialAttestation represents WEBAUTHN_CREDENTIAL_ATTESTATION
type webauthnCredentialAttestation struct {
	dwVersion               uint32
	pwszFormatType          *uint16
	cbAuthenticatorData     uint32
	pbAuthenticatorData     *byte
	cbAttestation           uint32
	pbAttestation           *byte
	dwAttestationDecodeType uint32
	pvAttestationDecode     uintptr
	cbAttestationObject     uint32
	pbAttestationObject     *byte
	cbCredentialId          uint32
	pbCredentialId          *byte
	extensions              uintptr
	dwUsedTransport         uint32
}

// webauthnAssertion represents WEBAUTHN_ASSERTION
type webauthnAssertion struct {
	dwVersion           uint32
	cbAuthenticatorData uint32
	pbAuthenticatorData *byte
	cbSignature         uint32
	pbSignature         *byte
	credential          webauthnCredential
	cbUserId            uint32
	pbUserId            *byte
}
