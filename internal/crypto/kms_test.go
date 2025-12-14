package crypto

import (
	"os"
	"testing"
)

func TestFileBasedKMSGenerateDataKey(t *testing.T) {
	// Create temporary key store
	tmpDir := t.TempDir()

	kms, err := NewFileBasedKMS(FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create FileBasedKMS: %v", err)
	}

	// Generate a data key
	plaintext1, ciphertext1, err := kms.GenerateDataKey(nil, "test-key-1")
	if err != nil {
		t.Fatalf("Failed to generate data key: %v", err)
	}

	if len(plaintext1) != 32 {
		t.Errorf("Expected plaintext length 32, got %d", len(plaintext1))
	}

	if len(ciphertext1) != 32 {
		t.Errorf("Expected ciphertext length 32, got %d", len(ciphertext1))
	}

	// Verify plaintext and ciphertext are different
	if string(plaintext1) == string(ciphertext1) {
		t.Error("Plaintext and ciphertext should not be equal")
	}

	// Generate another data key with the same master key
	plaintext2, ciphertext2, err := kms.GenerateDataKey(nil, "test-key-1")
	if err != nil {
		t.Fatalf("Failed to generate second data key: %v", err)
	}

	// Verify that different data keys are generated each time
	if string(plaintext1) == string(plaintext2) {
		t.Error("Two data keys should be different")
	}

	// Verify that ciphertexts are different (due to different plaintexts)
	if string(ciphertext1) == string(ciphertext2) {
		t.Error("Two ciphertexts should be different")
	}
}

func TestFileBasedKMSDecrypt(t *testing.T) {
	tmpDir := t.TempDir()

	kms, err := NewFileBasedKMS(FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create FileBasedKMS: %v", err)
	}

	// Generate and encrypt a data key
	plaintext, ciphertext, err := kms.GenerateDataKey(nil, "test-key-1")
	if err != nil {
		t.Fatalf("Failed to generate data key: %v", err)
	}

	// Decrypt the ciphertext
	decrypted, err := kms.Decrypt(nil, ciphertext, "test-key-1")
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}

	// Verify decryption recovered the plaintext
	if string(plaintext) != string(decrypted) {
		t.Error("Decrypted plaintext does not match original")
	}
}

func TestFileBasedKMSDecryptWithWrongKeyID(t *testing.T) {
	tmpDir := t.TempDir()

	kms, err := NewFileBasedKMS(FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create FileBasedKMS: %v", err)
	}

	// Generate a data key with one key ID
	_, ciphertext, err := kms.GenerateDataKey(nil, "test-key-1")
	if err != nil {
		t.Fatalf("Failed to generate data key: %v", err)
	}

	// Try to decrypt with a different key ID
	_, err = kms.Decrypt(nil, ciphertext, "test-key-2")
	if err == nil {
		t.Error("Expected error when decrypting with wrong key ID")
	}
}

func TestFileBasedKMSPersistence(t *testing.T) {
	tmpDir := t.TempDir()

	kms1, err := NewFileBasedKMS(FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create first FileBasedKMS: %v", err)
	}

	// Generate a data key
	plaintext, ciphertext, err := kms1.GenerateDataKey(nil, "test-key-1")
	if err != nil {
		t.Fatalf("Failed to generate data key: %v", err)
	}

	// Verify key file was created
	keyFile := tmpDir + "/test-key-1.key"
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("Key file was not created: %v", err)
	}

	// Create a new KMS instance with the same key store
	kms2, err := NewFileBasedKMS(FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create second FileBasedKMS: %v", err)
	}

	// Decrypt the ciphertext with the new instance
	decrypted, err := kms2.Decrypt(nil, ciphertext, "test-key-1")
	if err != nil {
		t.Fatalf("Failed to decrypt with second instance: %v", err)
	}

	// Verify decryption recovered the plaintext
	if string(plaintext) != string(decrypted) {
		t.Error("Decrypted plaintext does not match original across instances")
	}
}

func TestFileBasedKMSGetKeyID(t *testing.T) {
	tmpDir := t.TempDir()

	kms, err := NewFileBasedKMS(FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create FileBasedKMS: %v", err)
	}

	keyID, err := kms.GetKeyID(nil)
	if err != nil {
		t.Fatalf("Failed to get key ID: %v", err)
	}

	if keyID != "test-key-1" {
		t.Errorf("Expected key ID 'test-key-1', got '%s'", keyID)
	}
}

func TestXorEncrypt(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	key := []byte{0xFF, 0xFF, 0xFF, 0xFF}

	encrypted := xorEncrypt(data, key)
	if string(encrypted) == string(data) {
		t.Error("XOR encryption should change data")
	}

	// XOR is its own inverse
	decrypted := xorEncrypt(encrypted, key)
	if string(decrypted) != string(data) {
		t.Error("XOR decryption does not recover original data")
	}
}
