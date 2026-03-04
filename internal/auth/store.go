package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// APIKey represents a single API key entry.
type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"created_at"`
	Revoked   bool      `json:"revoked"`
}

// storeData is the JSON structure persisted to disk.
type storeData struct {
	AdminPassword string   `json:"admin_password"`
	APIKeys       []APIKey `json:"api_keys"`
}

// Store manages admin authentication and API keys.
// Admin password and API keys are stored in plaintext in a JSON file.
// This is a deliberate trade-off for simplicity in a personal-use application.
// Anyone with read access to the keys file has full access.
type Store struct {
	mu       sync.RWMutex
	filePath string
	data     storeData
	// sessions maps session token -> expiry time (in-memory only, lost on restart).
	sessions map[string]time.Time
}

// session lifetime for the admin dashboard.
const sessionLifetime = 24 * time.Hour

// NewStore creates a new auth store. It loads existing data from filePath if present.
// If adminPassword is non-empty, it overwrites the stored admin password.
// If no file exists and no password is provided, a random password is generated.
func NewStore(filePath string, adminPassword string) (*Store, error) {
	s := &Store{
		filePath: filePath,
		sessions: make(map[string]time.Time),
	}

	// Try to load existing file.
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load auth store: %w", err)
	}

	// Handle admin password.
	if adminPassword != "" {
		// Flag provided — overwrite whatever was stored.
		s.data.AdminPassword = adminPassword
		if err := s.save(); err != nil {
			return nil, fmt.Errorf("failed to save auth store: %w", err)
		}
		log.Printf("Admin password set from command-line flag")
	} else if s.data.AdminPassword == "" {
		// No flag, no stored password — generate one.
		generated, err := generateRandomString(16)
		if err != nil {
			return nil, fmt.Errorf("failed to generate admin password: %w", err)
		}
		s.data.AdminPassword = generated
		if err := s.save(); err != nil {
			return nil, fmt.Errorf("failed to save auth store: %w", err)
		}
		log.Printf("========================================")
		log.Printf("  Generated admin password: %s", generated)
		log.Printf("  Saved to: %s", filePath)
		log.Printf("========================================")
	}

	return s, nil
}

// ValidateAdminPassword checks whether the provided password matches.
func (s *Store) ValidateAdminPassword(password string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return password != "" && password == s.data.AdminPassword
}

// CreateSession generates a new session token for an authenticated admin.
func (s *Store) CreateSession() (string, error) {
	token, err := generateRandomString(32)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = time.Now().Add(sessionLifetime)
	return token, nil
}

// ValidateSession checks whether a session token is valid and not expired.
func (s *Store) ValidateSession(token string) bool {
	if token == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	expiry, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(s.sessions, token)
		return false
	}
	return true
}

// DestroySession removes a session token (logout).
func (s *Store) DestroySession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// CreateAPIKey creates a new API key with the given name and persists the store.
func (s *Store) CreateAPIKey(name string) (*APIKey, error) {
	id, err := generateRandomString(8)
	if err != nil {
		return nil, err
	}
	key, err := generateRandomString(32)
	if err != nil {
		return nil, err
	}

	apiKey := APIKey{
		ID:        id,
		Name:      name,
		Key:       "sk_" + key,
		CreatedAt: time.Now(),
		Revoked:   false,
	}

	s.mu.Lock()
	s.data.APIKeys = append(s.data.APIKeys, apiKey)
	s.mu.Unlock()

	if err := s.save(); err != nil {
		return nil, fmt.Errorf("failed to save auth store: %w", err)
	}

	log.Printf("API key created: %s (%s)", apiKey.Name, apiKey.ID)
	return &apiKey, nil
}

// RevokeAPIKey marks an API key as revoked by its ID.
func (s *Store) RevokeAPIKey(id string) error {
	s.mu.Lock()
	found := false
	for i := range s.data.APIKeys {
		if s.data.APIKeys[i].ID == id {
			s.data.APIKeys[i].Revoked = true
			found = true
			break
		}
	}
	s.mu.Unlock()

	if !found {
		return fmt.Errorf("API key not found: %s", id)
	}

	if err := s.save(); err != nil {
		return fmt.Errorf("failed to save auth store: %w", err)
	}

	log.Printf("API key revoked: %s", id)
	return nil
}

// DeleteAPIKey permanently removes an API key by its ID.
func (s *Store) DeleteAPIKey(id string) error {
	s.mu.Lock()
	found := false
	for i := range s.data.APIKeys {
		if s.data.APIKeys[i].ID == id {
			s.data.APIKeys = append(s.data.APIKeys[:i], s.data.APIKeys[i+1:]...)
			found = true
			break
		}
	}
	s.mu.Unlock()

	if !found {
		return fmt.Errorf("API key not found: %s", id)
	}

	if err := s.save(); err != nil {
		return fmt.Errorf("failed to save auth store: %w", err)
	}

	log.Printf("API key deleted: %s", id)
	return nil
}

// ListAPIKeys returns a copy of all API keys.
func (s *Store) ListAPIKeys() []APIKey {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]APIKey, len(s.data.APIKeys))
	copy(keys, s.data.APIKeys)
	return keys
}

// ValidateAPIKey checks whether the provided key matches any active (non-revoked) API key.
func (s *Store) ValidateAPIKey(key string) bool {
	if key == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, k := range s.data.APIKeys {
		if !k.Revoked && k.Key == key {
			return true
		}
	}
	return false
}

// load reads the store data from disk.
func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.data)
}

// save writes the store data to disk.
func (s *Store) save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0600)
}

// generateRandomString returns a hex-encoded random string of the given byte length.
func generateRandomString(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
