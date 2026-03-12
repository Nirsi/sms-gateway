package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// APIKey represents a single API key entry.
type APIKey struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	KeyHash    string    `json:"key_hash,omitempty"`
	KeyPreview string    `json:"key_preview,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	Revoked    bool      `json:"revoked"`
}

// storeData is the JSON structure persisted to disk.
type storeData struct {
	AdminPasswordHash string   `json:"admin_password_hash,omitempty"`
	APIKeys           []APIKey `json:"api_keys"`
}

type session struct {
	Expiry time.Time
}

// Store manages admin authentication and API keys.
type Store struct {
	mu       sync.RWMutex
	filePath string
	data     storeData
	// sessions are in-memory only and are lost on restart.
	sessions map[string]session
}

// session lifetime for the admin dashboard.
const sessionLifetime = 24 * time.Hour

const sessionCleanupInterval = 5 * time.Minute

// NewStore creates a new auth store. It loads existing data from filePath if present.
// If adminPassword is non-empty, it overwrites the stored admin password.
// If no file exists and no password is provided, a random password is generated.
func NewStore(filePath string, adminPassword string) (*Store, error) {
	s := &Store{
		filePath: filePath,
		sessions: make(map[string]session),
	}

	// Try to load existing file.
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load auth store: %w", err)
	}

	// Handle admin password.
	if adminPassword != "" {
		hash, err := hashAdminPassword(adminPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to hash admin password: %w", err)
		}
		s.data.AdminPasswordHash = hash
		if err := s.save(); err != nil {
			return nil, fmt.Errorf("failed to save auth store: %w", err)
		}
		log.Printf("Admin password set from configuration")
	} else if s.data.AdminPasswordHash == "" {
		// No flag, no stored password — generate one.
		generated, err := generateRandomString(16)
		if err != nil {
			return nil, fmt.Errorf("failed to generate admin password: %w", err)
		}
		hash, err := hashAdminPassword(generated)
		if err != nil {
			return nil, fmt.Errorf("failed to hash generated admin password: %w", err)
		}
		s.data.AdminPasswordHash = hash
		if err := s.save(); err != nil {
			return nil, fmt.Errorf("failed to save auth store: %w", err)
		}
		log.Printf("========================================")
		log.Printf("  Generated admin password: %s", generated)
		log.Printf("  Saved to: %s", filePath)
		log.Printf("========================================")
	}

	go s.cleanupSessions()

	return s, nil
}

// ValidateAdminPassword checks whether the provided password matches.
func (s *Store) ValidateAdminPassword(password string) bool {
	s.mu.RLock()
	hash := s.data.AdminPasswordHash
	defer s.mu.RUnlock()
	if password == "" || hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// CreateSession generates a new session token for an authenticated admin.
func (s *Store) CreateSession() (string, error) {
	token, err := generateRandomString(32)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = session{Expiry: time.Now().Add(sessionLifetime)}
	return token, nil
}

// ValidateSession checks whether a session token is valid and not expired.
func (s *Store) ValidateSession(token string) bool {
	if token == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(session.Expiry) {
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
func (s *Store) CreateAPIKey(name string) (*APIKey, string, error) {
	id, err := generateRandomString(8)
	if err != nil {
		return nil, "", err
	}
	key, err := generateRandomString(32)
	if err != nil {
		return nil, "", err
	}
	rawKey := "sk_" + key

	apiKey := APIKey{
		ID:         id,
		Name:       name,
		KeyHash:    hashAPIKey(rawKey),
		KeyPreview: previewAPIKey(rawKey),
		CreatedAt:  time.Now(),
		Revoked:    false,
	}

	s.mu.Lock()
	s.data.APIKeys = append(s.data.APIKeys, apiKey)
	s.mu.Unlock()

	if err := s.save(); err != nil {
		return nil, "", fmt.Errorf("failed to save auth store: %w", err)
	}

	log.Printf("API key created: %s (%s)", apiKey.Name, apiKey.ID)
	return &apiKey, rawKey, nil
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
		if !k.Revoked && subtle.ConstantTimeCompare([]byte(k.KeyHash), []byte(hashAPIKey(key))) == 1 {
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

	loaded := storeData{}
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}

	s.mu.Lock()
	s.data = loaded
	s.mu.Unlock()

	return nil
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

func (s *Store) cleanupSessions() {
	ticker := time.NewTicker(sessionCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for token, session := range s.sessions {
			if now.After(session.Expiry) {
				delete(s.sessions, token)
			}
		}
		s.mu.Unlock()
	}
}

func hashAdminPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func previewAPIKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..." + key[len(key)-4:]
}

// generateRandomString returns a hex-encoded random string of the given byte length.
func generateRandomString(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
