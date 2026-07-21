//go:build windows
// +build windows

package webview2

import (
	"sync"
	"time"
)

// StoredCredential represents a WebAuthn credential stored by CredentialStore implementations
type StoredCredential struct {
	ID         string
	RPID       string
	UserID     string
	UserName   string
	PublicKey  []byte    // COSE-encoded public key
	PrivateKey []byte    // Encrypted private key (implementation-specific)
	SignCount  uint32
	CreatedAt  time.Time
}

// CredentialStore defines the interface for storing and retrieving WebAuthn credentials.
// Implementations must handle credential persistence and retrieval for WebAuthn operations.
type CredentialStore interface {
	// Save stores a credential, updating it if it already exists
	Save(credential StoredCredential) error

	// Load retrieves a credential by its ID
	Load(credentialID string) (StoredCredential, error)

	// LoadAll retrieves all credentials for a given Relying Party ID
	LoadAll(rpID string) ([]StoredCredential, error)

	// Delete removes a credential by its ID
	Delete(credentialID string) error
}

// InMemoryCredentialStore is a simple in-memory implementation of CredentialStore.
// This is suitable for testing and demos but should not be used in production
// where credential persistence is required.
//
// Note: This implementation does NOT encrypt private keys. For production use,
// consider using the default encrypted file store or implementing your own
// secure storage.
type InMemoryCredentialStore struct {
	mu          sync.RWMutex
	credentials map[string]StoredCredential // Key is credential ID
}

// NewInMemoryCredentialStore creates a new in-memory credential store
func NewInMemoryCredentialStore() *InMemoryCredentialStore {
	return &InMemoryCredentialStore{
		credentials: make(map[string]StoredCredential),
	}
}

// Save stores a credential
func (s *InMemoryCredentialStore) Save(credential StoredCredential) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.credentials[credential.ID] = credential
	return nil
}

// Load retrieves a credential by its ID
func (s *InMemoryCredentialStore) Load(credentialID string) (StoredCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	credential, ok := s.credentials[credentialID]
	if !ok {
		return StoredCredential{}, ErrCredentialNotFound
	}

	return credential, nil
}

// LoadAll retrieves all credentials for a given Relying Party ID
func (s *InMemoryCredentialStore) LoadAll(rpID string) ([]StoredCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []StoredCredential
	for _, cred := range s.credentials {
		if cred.RPID == rpID {
			results = append(results, cred)
		}
	}

	return results, nil
}

// Delete removes a credential
func (s *InMemoryCredentialStore) Delete(credentialID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.credentials[credentialID]; !ok {
		return ErrCredentialNotFound
	}

	delete(s.credentials, credentialID)
	return nil
}
