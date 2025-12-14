package crypto

import (
	"bytes"
	"testing"
)

func TestAEADEncryptDecrypt(t *testing.T) {
	// Create a file-based KMS
	tmpDir := t.TempDir()
	kms, err := NewFileBasedKMS(FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create KMS: %v", err)
	}

	encryptor := NewAEADEncryptor(kms)

	plaintext := []byte("secret card data")
	additionalData := []byte("aad-data")
	keyID := "test-key-1"

	// Encrypt the plaintext
	encData, err := encryptor.Encrypt(nil, plaintext, keyID, additionalData)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	// Verify ciphertext is different from plaintext
	if bytes.Equal(encData.Ciphertext, plaintext) {
		t.Error("Ciphertext should not equal plaintext")
	}

	// Decrypt the ciphertext
	decrypted, err := encryptor.Decrypt(nil, encData)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	// Verify decryption recovered the plaintext
	if !bytes.Equal(decrypted, plaintext) {
		t.Error("Decrypted plaintext does not match original")
	}
}

func TestAEADEncryptionUniqueness(t *testing.T) {
	// Create a file-based KMS
	tmpDir := t.TempDir()
	kms, err := NewFileBasedKMS(FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create KMS: %v", err)
	}

	encryptor := NewAEADEncryptor(kms)

	plaintext := []byte("same secret data")
	keyID := "test-key-1"

	// Encrypt the same plaintext twice
	encData1, err := encryptor.Encrypt(nil, plaintext, keyID, []byte("aad1"))
	if err != nil {
		t.Fatalf("First encryption failed: %v", err)
	}

	encData2, err := encryptor.Encrypt(nil, plaintext, keyID, []byte("aad1"))
	if err != nil {
		t.Fatalf("Second encryption failed: %v", err)
	}

	// Verify ciphertexts are different (due to random nonces and data keys)
	if bytes.Equal(encData1.Ciphertext, encData2.Ciphertext) {
		t.Error("Two encryptions of the same plaintext should produce different ciphertexts")
	}

	// Verify nonces are different
	if bytes.Equal(encData1.Nonce, encData2.Nonce) {
		t.Error("Nonces should be different")
	}
}

func TestAEADAuthenticationFailure(t *testing.T) {
	// Create a file-based KMS
	tmpDir := t.TempDir()
	kms, err := NewFileBasedKMS(FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create KMS: %v", err)
	}

	encryptor := NewAEADEncryptor(kms)

	plaintext := []byte("secret data")
	keyID := "test-key-1"

	// Encrypt with one AAD
	encData, err := encryptor.Encrypt(nil, plaintext, keyID, []byte("original-aad"))
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	// Change the AAD
	encData.AdditionalData = []byte("tampered-aad")

	// Decryption should fail
	_, err = encryptor.Decrypt(nil, encData)
	if err == nil {
		t.Error("Decryption should fail with tampered AAD")
	}
}

func TestAEADCiphertextTampering(t *testing.T) {
	// Create a file-based KMS
	tmpDir := t.TempDir()
	kms, err := NewFileBasedKMS(FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create KMS: %v", err)
	}

	encryptor := NewAEADEncryptor(kms)

	plaintext := []byte("secret data")
	keyID := "test-key-1"

	// Encrypt the plaintext
	encData, err := encryptor.Encrypt(nil, plaintext, keyID, []byte("aad"))
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	// Tamper with the ciphertext
	if len(encData.Ciphertext) > 0 {
		encData.Ciphertext[0] ^= 0xFF
	}

	// Decryption should fail
	_, err = encryptor.Decrypt(nil, encData)
	if err == nil {
		t.Error("Decryption should fail with tampered ciphertext")
	}
}

func TestAEADNonceSize(t *testing.T) {
	// Create a file-based KMS
	tmpDir := t.TempDir()
	kms, err := NewFileBasedKMS(FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create KMS: %v", err)
	}

	encryptor := NewAEADEncryptor(kms)

	plaintext := []byte("secret data")
	keyID := "test-key-1"

	// Encrypt the plaintext
	encData, err := encryptor.Encrypt(nil, plaintext, keyID, []byte("aad"))
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	// Verify nonce size (GCM uses 12 bytes)
	if len(encData.Nonce) != 12 {
		t.Errorf("Expected nonce size 12, got %d", len(encData.Nonce))
	}
}
