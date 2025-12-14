package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// AEADEncryptor provides AES-256-GCM envelope encryption with per-record data keys.
type AEADEncryptor struct {
	kms KMS
}

// NewAEADEncryptor creates a new AEAD encryptor with the given KMS.
func NewAEADEncryptor(kms KMS) *AEADEncryptor {
	return &AEADEncryptor{
		kms: kms,
	}
}

// EncryptedData holds the encrypted plaintext with metadata.
type EncryptedData struct {
	Ciphertext         []byte // Encrypted payload
	EncryptedDataKey   []byte // Data key encrypted with master key
	Nonce              []byte // GCM nonce (12 bytes)
	KeyID              string // Master key identifier
	AdditionalData     []byte // Additional authenticated data
}

// Encrypt encrypts plaintext using AES-256-GCM with a per-record data key.
// Returns the encrypted data and the encrypted data key.
func (a *AEADEncryptor) Encrypt(ctx interface{}, plaintext []byte, keyID string, additionalData []byte) (*EncryptedData, error) {
	// Generate a data key
	dataKeyPlaintext, dataKeyCiphertext, err := a.kms.GenerateDataKey(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate data key: %w", err)
	}

	// Create AES cipher block
	block, err := aes.NewCipher(dataKeyPlaintext)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate a random nonce (12 bytes for GCM)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt the plaintext
	ciphertext := gcm.Seal(nil, nonce, plaintext, additionalData)

	return &EncryptedData{
		Ciphertext:       ciphertext,
		EncryptedDataKey: dataKeyCiphertext,
		Nonce:            nonce,
		KeyID:            keyID,
		AdditionalData:   additionalData,
	}, nil
}

// Decrypt decrypts the ciphertext using the encrypted data key.
func (a *AEADEncryptor) Decrypt(ctx interface{}, encryptedData *EncryptedData) ([]byte, error) {
	// Decrypt the data key
	dataKeyPlaintext, err := a.kms.Decrypt(ctx, encryptedData.EncryptedDataKey, encryptedData.KeyID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data key: %w", err)
	}

	// Create AES cipher block
	block, err := aes.NewCipher(dataKeyPlaintext)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt the ciphertext
	plaintext, err := gcm.Open(nil, encryptedData.Nonce, encryptedData.Ciphertext, encryptedData.AdditionalData)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}
