//go:build windows
// +build windows

package webview2

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileCredentialStore implements encrypted file-based credential storage.
// The AES-256 key is randomly generated and protected by Windows DPAPI (CryptProtectData).
// The DPAPI-protected key blob is stored alongside the encrypted credentials file.
type fileCredentialStore struct {
	mu            sync.RWMutex
	filePath      string
	encryptionKey []byte
}

// newFileCredentialStore creates a new file-based credential store.
//
// Files created:
//   - %APPDATA%\go-webview2\webauthn_credentials.enc — AES-GCM encrypted credentials
//   - %APPDATA%\go-webview2\webauthn_credentials.key — DPAPI-protected AES key blob
//
// The AES key is randomly generated on first use and protected by Windows DPAPI,
// binding it to the current Windows user session.
func newFileCredentialStore() (*fileCredentialStore, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = os.Getenv("USERPROFILE")
		if appData != "" {
			appData = filepath.Join(appData, "AppData", "Roaming")
		}
	}
	if appData == "" {
		return nil, ErrAppDataNotFound
	}

	dir := filepath.Join(appData, "go-webview2")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	key, err := loadOrCreateDPAPIKey(filepath.Join(dir, "webauthn_credentials.key"))
	if err != nil {
		return nil, err
	}

	return &fileCredentialStore{
		filePath:      filepath.Join(dir, "webauthn_credentials.enc"),
		encryptionKey: key,
	}, nil
}

// loadOrCreateDPAPIKey loads an existing DPAPI-protected AES key or generates a new one.
func loadOrCreateDPAPIKey(keyPath string) ([]byte, error) {
	blob, err := os.ReadFile(keyPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if os.IsNotExist(err) {
		return generateAndStoreDPAPIKey(keyPath)
	}
	return dpapiDecrypt(blob)
}

// generateAndStoreDPAPIKey generates a random 32-byte AES key, protects it with DPAPI, and stores it.
func generateAndStoreDPAPIKey(keyPath string) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	protected, err := dpapiEncrypt(key)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, protected, 0600); err != nil {
		return nil, err
	}
	return key, nil
}

// dpapiEncrypt protects data using Windows DPAPI (CryptProtectData).
// The result is bound to the current Windows user session.
func dpapiEncrypt(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, ErrEmptyData
	}
	dataIn := windows.DataBlob{
		Size: uint32(len(data)),
		Data: unsafe.SliceData(data),
	}
	var dataOut windows.DataBlob
	if err := windows.CryptProtectData(&dataIn, nil, nil, 0, nil, 0, &dataOut); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(dataOut.Data)))
	result := make([]byte, dataOut.Size)
	copy(result, unsafe.Slice((*byte)(unsafe.Pointer(dataOut.Data)), dataOut.Size))
	return result, nil
}

// dpapiDecrypt unprotects data using Windows DPAPI (CryptUnprotectData).
func dpapiDecrypt(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, ErrEmptyData
	}
	dataIn := windows.DataBlob{
		Size: uint32(len(data)),
		Data: unsafe.SliceData(data),
	}
	var dataOut windows.DataBlob
	if err := windows.CryptUnprotectData(&dataIn, nil, nil, 0, nil, 0, &dataOut); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(dataOut.Data)))
	result := make([]byte, dataOut.Size)
	copy(result, unsafe.Slice((*byte)(unsafe.Pointer(dataOut.Data)), dataOut.Size))
	return result, nil
}

// Save stores a credential (implements CredentialStore interface)
func (s *fileCredentialStore) Save(cred StoredCredential) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.loadAllInternal()
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	found := false
	for i, c := range creds {
		if c.ID == cred.ID {
			creds[i] = cred
			found = true
			break
		}
	}
	if !found {
		creds = append(creds, cred)
	}

	return s.saveAllInternal(creds)
}

// Load retrieves a credential by ID (implements CredentialStore interface)
func (s *fileCredentialStore) Load(credentialID string) (StoredCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.loadAllInternal()
	if err != nil {
		return StoredCredential{}, err
	}

	for _, cred := range creds {
		if cred.ID == credentialID {
			return cred, nil
		}
	}

	return StoredCredential{}, ErrCredentialNotFound
}

// LoadAll retrieves all credentials for a given RP ID (implements CredentialStore interface)
func (s *fileCredentialStore) LoadAll(rpID string) ([]StoredCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.loadAllInternal()
	if err != nil {
		return nil, err
	}

	var result []StoredCredential
	for _, cred := range creds {
		if cred.RPID == rpID {
			result = append(result, cred)
		}
	}

	return result, nil
}

// Delete removes a credential (implements CredentialStore interface)
func (s *fileCredentialStore) Delete(credentialID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.loadAllInternal()
	if err != nil {
		return err
	}

	found := false
	newCreds := make([]StoredCredential, 0, len(creds))
	for _, cred := range creds {
		if cred.ID != credentialID {
			newCreds = append(newCreds, cred)
		} else {
			found = true
		}
	}

	if !found {
		return ErrCredentialNotFound
	}

	return s.saveAllInternal(newCreds)
}

// loadAllInternal loads all credentials without locking (caller must hold lock)
func (s *fileCredentialStore) loadAllInternal() ([]StoredCredential, error) {
	encryptedData, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []StoredCredential{}, nil
		}
		return nil, err
	}

	plaintext, err := s.decrypt(encryptedData)
	if err != nil {
		return nil, err
	}

	var creds []StoredCredential
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return nil, err
	}

	return creds, nil
}

// saveAllInternal saves all credentials without locking (caller must hold lock)
func (s *fileCredentialStore) saveAllInternal(creds []StoredCredential) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}

	encryptedData, err := s.encrypt(data)
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, encryptedData, 0600)
}

// encrypt encrypts data using AES-GCM
func (s *fileCredentialStore) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt decrypts data using AES-GCM
func (s *fileCredentialStore) decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrCiphertextTooShort
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
