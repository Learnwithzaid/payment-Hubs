package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// KMS defines the interface for key management operations.
type KMS interface {
	GenerateDataKey(ctx interface{}, keyID string) (plaintext, ciphertext []byte, err error)
	Decrypt(ctx interface{}, ciphertext []byte, keyID string) ([]byte, error)
	GetKeyID(ctx interface{}) (string, error)
}

// AWSKMSConfig holds configuration for AWS KMS client.
type AWSKMSConfig struct {
	KeyARN string
}

// AWSKMS implements KMS using AWS Key Management Service.
type AWSKMS struct {
	keyARN string
}

// NewAWSKMS creates a new AWS KMS client.
func NewAWSKMS(cfg AWSKMSConfig) *AWSKMS {
	return &AWSKMS{
		keyARN: cfg.KeyARN,
	}
}

// GenerateDataKey generates a data key using AWS KMS (stub for now).
func (a *AWSKMS) GenerateDataKey(ctx interface{}, keyID string) (plaintext, ciphertext []byte, err error) {
	return nil, nil, errors.New("AWS KMS not implemented in this context")
}

// Decrypt decrypts data using AWS KMS (stub for now).
func (a *AWSKMS) Decrypt(ctx interface{}, ciphertext []byte, keyID string) ([]byte, error) {
	return nil, errors.New("AWS KMS not implemented in this context")
}

// GetKeyID returns the AWS KMS key ARN.
func (a *AWSKMS) GetKeyID(ctx interface{}) (string, error) {
	return a.keyARN, nil
}

// FileBasedKMSConfig holds configuration for file-based KMS mock.
type FileBasedKMSConfig struct {
	KeyStorePath string
}

// FileBasedKMS implements KMS using local file storage for testing.
type FileBasedKMS struct {
	keyStorePath string
	keys         map[string][]byte
	mu            sync.RWMutex
}

// NewFileBasedKMS creates a new file-based KMS mock.
func NewFileBasedKMS(cfg FileBasedKMSConfig) (*FileBasedKMS, error) {
	kms := &FileBasedKMS{
		keyStorePath: cfg.KeyStorePath,
		keys:         make(map[string][]byte),
	}

	if err := os.MkdirAll(cfg.KeyStorePath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key store directory: %w", err)
	}

	return kms, nil
}

// GenerateDataKey generates a data key and encrypts it with the master key.
// Returns plaintext data key and encrypted data key.
func (f *FileBasedKMS) GenerateDataKey(ctx interface{}, keyID string) (plaintext, ciphertext []byte, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Get or create the master key
	masterKey, exists := f.keys[keyID]
	if !exists {
		if keyID == "" {
			return nil, nil, errors.New("key ID must not be empty")
		}
		// Create a new master key
		masterKey = make([]byte, 32) // 256-bit key
		if _, err := io.ReadFull(rand.Reader, masterKey); err != nil {
			return nil, nil, fmt.Errorf("failed to generate master key: %w", err)
		}
		f.keys[keyID] = masterKey

		// Persist the master key to disk
		if err := f.persistKey(keyID, masterKey); err != nil {
			return nil, nil, fmt.Errorf("failed to persist master key: %w", err)
		}
	}

	// Generate a data key (256-bit)
	plaintext = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, plaintext); err != nil {
		return nil, nil, fmt.Errorf("failed to generate data key: %w", err)
	}

	// Encrypt the data key with the master key using simple XOR for mock purposes
	// In production, this would use proper key encryption
	ciphertext = xorEncrypt(plaintext, masterKey)

	return plaintext, ciphertext, nil
}

// Decrypt decrypts an encrypted data key using the master key.
func (f *FileBasedKMS) Decrypt(ctx interface{}, ciphertext []byte, keyID string) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	masterKey, exists := f.keys[keyID]
	if !exists {
		return nil, fmt.Errorf("master key not found for key ID: %s", keyID)
	}

	plaintext := xorEncrypt(ciphertext, masterKey)
	return plaintext, nil
}

// GetKeyID returns a test key ID.
func (f *FileBasedKMS) GetKeyID(ctx interface{}) (string, error) {
	return "test-key-1", nil
}

// persistKey writes a master key to disk.
func (f *FileBasedKMS) persistKey(keyID string, key []byte) error {
	filename := fmt.Sprintf("%s/%s.key", f.keyStorePath, keyID)
	hexKey := hex.EncodeToString(key)
	return os.WriteFile(filename, []byte(hexKey), 0600)
}

// xorEncrypt performs simple XOR encryption (for mock purposes only).
func xorEncrypt(data, key []byte) []byte {
	result := make([]byte, len(data))
	for i := range data {
		result[i] = data[i] ^ key[i%len(key)]
	}
	return result
}
