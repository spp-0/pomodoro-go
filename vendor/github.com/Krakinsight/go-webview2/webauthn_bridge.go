package webview2

// webauthnBridgeJS is the JavaScript code that intercepts WebAuthn API calls
const webauthnBridgeJS = `
(function() {
	'use strict';

	// Store the original credentials API if it exists
	const originalCredentials = navigator.credentials;

	// Helper function to convert ArrayBuffer to base64url
	function arrayBufferToBase64Url(buffer) {
		const bytes = new Uint8Array(buffer);
		let binary = '';
		for (let i = 0; i < bytes.byteLength; i++) {
			binary += String.fromCharCode(bytes[i]);
		}
		return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
	}

	// Helper function to convert base64url to ArrayBuffer
	function base64UrlToArrayBuffer(base64url) {
		const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/');
		const padding = '='.repeat((4 - base64.length % 4) % 4);
		const binary = atob(base64 + padding);
		const bytes = new Uint8Array(binary.length);
		for (let i = 0; i < binary.length; i++) {
			bytes[i] = binary.charCodeAt(i);
		}
		return bytes.buffer;
	}

	// Helper to convert options for transmission to Go
	function serializeCreateOptions(options) {
		const serialized = {
			challenge: arrayBufferToBase64Url(options.challenge),
			rp: options.rp,
			user: {
				id: arrayBufferToBase64Url(options.user.id),
				name: options.user.name,
				displayName: options.user.displayName
			},
			pubKeyCredParams: options.pubKeyCredParams || [],
			origin: window.location.origin  // Pass actual page origin
		};

		if (options.authenticatorSelection) {
			serialized.authenticatorSelection = options.authenticatorSelection;
		}
		if (options.excludeCredentials && options.excludeCredentials.length > 0) {
			serialized.excludeCredentials = options.excludeCredentials.map(cred =>
				arrayBufferToBase64Url(cred.id)
			);
		}
		if (options.timeout) {
			serialized.timeout = options.timeout;
		}
		if (options.attestation) {
			serialized.attestation = options.attestation;
		}

		return serialized;
	}

	function serializeGetOptions(options) {
		const serialized = {
			challenge: arrayBufferToBase64Url(options.challenge),
			rpId: options.rpId || window.location.hostname,
			timeout: options.timeout || 60000,
			userVerification: options.userVerification || 'preferred',
			origin: window.location.origin  // Pass actual page origin
		};

		if (options.allowCredentials && options.allowCredentials.length > 0) {
			serialized.allowCredentials = options.allowCredentials.map(cred =>
				arrayBufferToBase64Url(cred.id)
			);
		} else {
			serialized.allowCredentials = [];
		}

		return serialized;
	}

	// Create a new credentials API
	const webauthnBridge = {
		async create(options) {
			if (!options || !options.publicKey) {
				throw new DOMException('Invalid options', 'NotSupportedError');
			}

			try {
				// Serialize options for Go
				const serializedOptions = serializeCreateOptions(options.publicKey);

				// Call Go handler
				const resultJSON = await window.__webauthn_create(JSON.stringify(serializedOptions));
				const result = JSON.parse(resultJSON);

				// Convert back to WebAuthn format
				const credential = {
					id: result.id,
					rawId: base64UrlToArrayBuffer(result.rawId),
					type: result.type || 'public-key',
					response: {
						clientDataJSON: base64UrlToArrayBuffer(result.response.clientDataJSON),
						attestationObject: base64UrlToArrayBuffer(result.response.attestationObject)
					}
				};

				// Add getClientExtensionResults method
				credential.getClientExtensionResults = function() { return {}; };

				return credential;
			} catch (error) {
				console.error('WebAuthn create error:', error);
				throw new DOMException(error.message || 'Create failed', 'NotAllowedError');
			}
		},

		async get(options) {
			if (!options || !options.publicKey) {
				throw new DOMException('Invalid options', 'NotSupportedError');
			}

			try {
				// Serialize options for Go
				const serializedOptions = serializeGetOptions(options.publicKey);

				// Call Go handler
				const resultJSON = await window.__webauthn_get(JSON.stringify(serializedOptions));
				const result = JSON.parse(resultJSON);

				// Convert back to WebAuthn format
				const assertion = {
					id: result.id,
					rawId: base64UrlToArrayBuffer(result.rawId),
					type: result.type || 'public-key',
					response: {
						clientDataJSON: base64UrlToArrayBuffer(result.response.clientDataJSON),
						authenticatorData: base64UrlToArrayBuffer(result.response.authenticatorData),
						signature: base64UrlToArrayBuffer(result.response.signature)
					}
				};

				if (result.response.userHandle) {
					assertion.response.userHandle = base64UrlToArrayBuffer(result.response.userHandle);
				}

				// Add getClientExtensionResults method
				assertion.getClientExtensionResults = function() { return {}; };

				return assertion;
			} catch (error) {
				console.error('WebAuthn get error:', error);
				throw new DOMException(error.message || 'Get failed', 'NotAllowedError');
			}
		},

		async preventSilentAccess() {
			// No-op for now
			return;
		}
	};

	// Override navigator.credentials
	Object.defineProperty(navigator, 'credentials', {
		value: webauthnBridge,
		writable: false,
		configurable: true
	});

	// Also check if PublicKeyCredential is available
	if (typeof window.PublicKeyCredential === 'undefined') {
		window.PublicKeyCredential = function() {};
		window.PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable = async function() {
			try {
				return await window.__webauthn_isAvailable();
			} catch (error) {
				return true; // Default to available
			}
		};
		window.PublicKeyCredential.isConditionalMediationAvailable = async function() {
			return false; // Not supported yet
		};
	}

	console.log('WebAuthn bridge initialized');
})();
`
