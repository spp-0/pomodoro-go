package webview2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"fmt"
	"math/big"

	"github.com/fxamacker/cbor/v2"
)

// Helper functions for base64url encoding/decoding
func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// encodeCOSEPublicKey encodes an ECDSA P-256 public key in COSE format
// COSE Key format according to RFC 8152
func encodeCOSEPublicKey(pubKey *ecdsa.PublicKey) ([]byte, error) {
	// COSE Key for ES256 (ECDSA with P-256 and SHA-256)
	// This is a CBOR-encoded map with the following structure:
	// {
	//   1: 2,        // kty: EC2 key type
	//   3: -7,       // alg: ES256
	//   -1: 1,       // crv: P-256
	//   -2: x,       // x coordinate (32 bytes)
	//   -3: y        // y coordinate (32 bytes)
	// }

	x := pubKey.X.Bytes()
	y := pubKey.Y.Bytes()

	// Ensure coordinates are 32 bytes (pad with leading zeros if needed)
	xBytes := make([]byte, 32)
	yBytes := make([]byte, 32)
	copy(xBytes[32-len(x):], x)
	copy(yBytes[32-len(y):], y)

	// Create COSE key map with integer keys (CBOR allows this)
	coseKey := map[int]interface{}{
		1:  2,      // kty: EC2
		3:  -7,     // alg: ES256
		-1: 1,      // crv: P-256
		-2: xBytes, // x coordinate
		-3: yBytes, // y coordinate
	}

	// Use canonical CBOR encoding
	encMode, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		return nil, fmt.Errorf("failed to create CBOR encode mode: %w", err)
	}

	encoded, err := encMode.Marshal(coseKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encode COSE key: %w", err)
	}

	return encoded, nil
}

// encodeECDSAPrivateKey encodes an ECDSA private key for storage
func encodeECDSAPrivateKey(privKey *ecdsa.PrivateKey) []byte {
	// Store the D value (private key scalar)
	d := privKey.D.Bytes()
	// Ensure it's 32 bytes for P-256
	privateKeyBytes := make([]byte, 32)
	copy(privateKeyBytes[32-len(d):], d)
	return privateKeyBytes
}

// decodeECDSAPrivateKey decodes a stored ECDSA private key
func decodeECDSAPrivateKey(keyBytes []byte) (*ecdsa.PrivateKey, error) {
	if len(keyBytes) != 32 {
		return nil, ErrInvalidPrivateKeyLength
	}

	privKey := new(ecdsa.PrivateKey)
	privKey.PublicKey.Curve = elliptic.P256()
	privKey.D = new(big.Int).SetBytes(keyBytes)

	// Derive public key from private key
	privKey.PublicKey.X, privKey.PublicKey.Y = elliptic.P256().ScalarBaseMult(keyBytes)

	return privKey, nil
}

// signWithECDSA creates an ECDSA signature over the data
func signWithECDSA(privKey *ecdsa.PrivateKey, data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)

	r, s, err := ecdsa.Sign(rand.Reader, privKey, hash[:])
	if err != nil {
		return nil, err
	}

	// Encode signature in ASN.1 DER format (standard for WebAuthn)
	type ECDSASignature struct {
		R, S *big.Int
	}
	sig := ECDSASignature{R: r, S: s}
	return asn1.Marshal(sig)
}

// randomBytes fills the byte slice with random data
func randomBytes(b []byte) (int, error) {
	return rand.Read(b)
}
