package vault

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/example/pci-infra/internal/crypto"
)

func TestIntegrationFullWorkflow(t *testing.T) {
	// Setup database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	// Run migrations
	migrationSQL := `
	CREATE TABLE vault_cards (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT UNIQUE NOT NULL,
		first6 TEXT NOT NULL,
		last4 TEXT NOT NULL,
		expiry TEXT NOT NULL,
		cardholder TEXT NOT NULL,
		ciphertext BLOB NOT NULL,
		encrypted_key BLOB NOT NULL,
		nonce BLOB NOT NULL,
		key_id TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX idx_vault_cards_token ON vault_cards(token);
	CREATE INDEX idx_vault_cards_first6_last4 ON vault_cards(first6, last4);
	CREATE INDEX idx_vault_cards_key_id ON vault_cards(key_id);
	CREATE INDEX idx_vault_cards_created_at ON vault_cards(created_at);

	CREATE TABLE vault_key_rotations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		old_key_id TEXT NOT NULL,
		new_key_id TEXT NOT NULL,
		rotated_count INTEGER NOT NULL,
		rotated_at TIMESTAMP NOT NULL
	);

	CREATE INDEX idx_vault_key_rotations_timestamp ON vault_key_rotations(rotated_at);

	CREATE TABLE vault_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key_id TEXT UNIQUE NOT NULL,
		created_at TIMESTAMP NOT NULL,
		revoked_at TIMESTAMP,
		status TEXT DEFAULT 'active'
	);

	CREATE INDEX idx_vault_keys_status ON vault_keys(status);
	`

	_, err = db.Exec(migrationSQL)
	if err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Setup KMS and encryptor
	tmpDir := t.TempDir()
	kms, err := crypto.NewFileBasedKMS(crypto.FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create KMS: %v", err)
	}

	encryptor := crypto.NewAEADEncryptor(kms)
	tokenizer := NewTokenizer()
	store := NewVaultStore(db, encryptor, tokenizer)
	service := NewVaultService(store)

	ctx := context.Background()

	// Test 1: Tokenize a card
	token, first6, last4, err := service.TokenizeCard(ctx, "4532015112830366", "123", "12/25", "John Doe")
	if err != nil {
		t.Fatalf("TokenizeCard failed: %v", err)
	}

	if token == "" || first6 != "453201" || last4 != "0366" {
		t.Errorf("TokenizeCard returned invalid data: token=%s, first6=%s, last4=%s", token, first6, last4)
	}

	// Test 2: Detokenize the card
	pan, cvv, expiry, cardholder, err := service.DetokenizeCard(ctx, token)
	if err != nil {
		t.Fatalf("DetokenizeCard failed: %v", err)
	}

	if pan != "4532015112830366" || cvv != "123" || expiry != "12/25" || cardholder != "John Doe" {
		t.Errorf("DetokenizeCard returned invalid data: pan=%s, cvv=%s, expiry=%s, cardholder=%s",
			pan, cvv, expiry, cardholder)
	}

	// Test 3: Verify ciphertext uniqueness
	token2, _, _, err := service.TokenizeCard(ctx, "4532015112830366", "123", "12/25", "John Doe")
	if err != nil {
		t.Fatalf("Second TokenizeCard failed: %v", err)
	}

	if token == token2 {
		t.Error("Two tokenizations should produce different tokens")
	}

	// Verify the tokens have different ciphertexts
	card1, _ := store.RetrieveCard(ctx, token)
	card2, _ := store.RetrieveCard(ctx, token2)

	if len(card1.Ciphertext) == 0 || len(card2.Ciphertext) == 0 {
		t.Fatal("Ciphertexts should not be empty")
	}

	// Compare ciphertexts (they should be different due to random nonces and data keys)
	if string(card1.Ciphertext) == string(card2.Ciphertext) {
		t.Error("Two tokenizations should produce different ciphertexts")
	}

	// Test 4: Verify no plaintext in database
	var storedCiphertext string
	err = db.QueryRow("SELECT ciphertext FROM vault_cards WHERE token = ?", token).Scan(&storedCiphertext)
	if err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}

	// Verify PAN is not in plaintext
	if storedCiphertext == "4532015112830366" {
		t.Error("PAN should not be stored as plaintext in database")
	}

	// Test 5: Verify partial PAN storage
	var storedFirst6, storedLast4 string
	err = db.QueryRow("SELECT first6, last4 FROM vault_cards WHERE token = ?", token).Scan(&storedFirst6, &storedLast4)
	if err != nil {
		t.Fatalf("Failed to query PAN parts: %v", err)
	}

	if storedFirst6 != "453201" || storedLast4 != "0366" {
		t.Errorf("Partial PAN storage failed: first6=%s, last4=%s", storedFirst6, storedLast4)
	}
}

func TestIntegrationInvalidCardDataRejection(t *testing.T) {
	// Setup
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	// Run migrations
	migrationSQL := `
	CREATE TABLE vault_cards (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT UNIQUE NOT NULL,
		first6 TEXT NOT NULL,
		last4 TEXT NOT NULL,
		expiry TEXT NOT NULL,
		cardholder TEXT NOT NULL,
		ciphertext BLOB NOT NULL,
		encrypted_key BLOB NOT NULL,
		nonce BLOB NOT NULL,
		key_id TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	db.Exec(migrationSQL)

	tmpDir := t.TempDir()
	kms, _ := crypto.NewFileBasedKMS(crypto.FileBasedKMSConfig{KeyStorePath: tmpDir})
	encryptor := crypto.NewAEADEncryptor(kms)
	tokenizer := NewTokenizer()
	store := NewVaultStore(db, encryptor, tokenizer)
	service := NewVaultService(store)

	ctx := context.Background()

	// Test invalid PAN
	_, _, _, err = service.TokenizeCard(ctx, "1234567890123456", "123", "12/25", "John Doe")
	if err == nil {
		t.Error("Should reject invalid PAN")
	}

	// Test invalid CVV
	_, _, _, err = service.TokenizeCard(ctx, "4532015112830366", "12", "12/25", "John Doe")
	if err == nil {
		t.Error("Should reject invalid CVV")
	}

	// Test invalid expiry
	_, _, _, err = service.TokenizeCard(ctx, "4532015112830366", "123", "13/25", "John Doe")
	if err == nil {
		t.Error("Should reject invalid expiry")
	}

	// Test empty cardholder
	_, _, _, err = service.TokenizeCard(ctx, "4532015112830366", "123", "12/25", "")
	if err == nil {
		t.Error("Should reject empty cardholder")
	}
}

func TestIntegrationKeyRotation(t *testing.T) {
	// Setup database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	// Run migrations
	migrationSQL := `
	CREATE TABLE vault_cards (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT UNIQUE NOT NULL,
		first6 TEXT NOT NULL,
		last4 TEXT NOT NULL,
		expiry TEXT NOT NULL,
		cardholder TEXT NOT NULL,
		ciphertext BLOB NOT NULL,
		encrypted_key BLOB NOT NULL,
		nonce BLOB NOT NULL,
		key_id TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX idx_vault_cards_key_id ON vault_cards(key_id);
	`
	db.Exec(migrationSQL)

	// Setup KMS and encryptor
	tmpDir := t.TempDir()
	kms, _ := crypto.NewFileBasedKMS(crypto.FileBasedKMSConfig{KeyStorePath: tmpDir})
	encryptor := crypto.NewAEADEncryptor(kms)
	tokenizer := NewTokenizer()
	store := NewVaultStore(db, encryptor, tokenizer)

	ctx := context.Background()

	// Store some cards with test-key-1
	oldKeyID := "test-key-1"
	tokens := []string{}

	for i := 0; i < 3; i++ {
		result, _ := store.StoreCard(ctx, "4532015112830366", "123", "12/25", "John Doe")
		tokens = append(tokens, result.Token)
	}

	// Verify all cards use the old key
	for _, token := range tokens {
		card, _ := store.RetrieveCard(ctx, token)
		if card.KeyID != oldKeyID {
			t.Errorf("Card should use old key, got: %s", card.KeyID)
		}
	}

	// Rotate keys
	newKeyID := "test-key-2"
	rotatedCount, err := store.RotateKey(ctx, oldKeyID, newKeyID)
	if err != nil {
		t.Fatalf("RotateKey failed: %v", err)
	}

	if rotatedCount != 3 {
		t.Errorf("Expected 3 cards rotated, got %d", rotatedCount)
	}

	// Verify all cards now use the new key
	for _, token := range tokens {
		card, _ := store.RetrieveCard(ctx, token)
		if card.KeyID != newKeyID {
			t.Errorf("Card should use new key, got: %s", card.KeyID)
		}

		// Verify decryption still works
		pan, _, _, _, err := store.DecryptCard(ctx, card)
		if err != nil {
			t.Fatalf("Failed to decrypt after rotation: %v", err)
		}

		if pan != "4532015112830366" {
			t.Errorf("Decrypted PAN incorrect: %s", pan)
		}
	}
}
