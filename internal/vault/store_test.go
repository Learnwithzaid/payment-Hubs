package vault

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/example/pci-infra/internal/crypto"
)

func setupTestDatabase(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

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

	return db
}

func setupVaultStore(t *testing.T) (*VaultStore, *sql.DB) {
	db := setupTestDatabase(t)

	tmpDir := t.TempDir()
	kms, err := crypto.NewFileBasedKMS(crypto.FileBasedKMSConfig{KeyStorePath: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create KMS: %v", err)
	}

	encryptor := crypto.NewAEADEncryptor(kms)
	tokenizer := NewTokenizer()
	store := NewVaultStore(db, encryptor, tokenizer)

	return store, db
}

func TestStoreCardNoPlaintextOnDisk(t *testing.T) {
	store, db := setupVaultStore(t)
	defer db.Close()

	ctx := context.Background()

	// Store a card
	result, err := store.StoreCard(ctx, "4532015112830366", "123", "12/25", "John Doe")
	if err != nil {
		t.Fatalf("Failed to store card: %v", err)
	}

	// Verify the card was stored
	if result.Token == "" {
		t.Error("Token should not be empty")
	}

	if result.First6 != "453201" {
		t.Errorf("Expected first6 '453201', got '%s'", result.First6)
	}

	if result.Last4 != "0366" {
		t.Errorf("Expected last4 '0366', got '%s'", result.Last4)
	}

	// Verify ciphertext is stored, not plaintext
	if len(result.Ciphertext) == 0 {
		t.Error("Ciphertext should not be empty")
	}

	// Verify no plaintext card data is in the database
	var storedCiphertext string
	err = db.QueryRow("SELECT ciphertext FROM vault_cards WHERE token = ?", result.Token).Scan(&storedCiphertext)
	if err != nil {
		t.Fatalf("Failed to query stored card: %v", err)
	}

	// Check that PAN is not in the database
	if storedCiphertext == "4532015112830366" {
		t.Error("PAN should not be stored as plaintext")
	}
}

func TestRetrieveCard(t *testing.T) {
	store, db := setupVaultStore(t)
	defer db.Close()

	ctx := context.Background()

	// Store a card
	stored, err := store.StoreCard(ctx, "4532015112830366", "123", "12/25", "John Doe")
	if err != nil {
		t.Fatalf("Failed to store card: %v", err)
	}

	// Retrieve the card
	retrieved, err := store.RetrieveCard(ctx, stored.Token)
	if err != nil {
		t.Fatalf("Failed to retrieve card: %v", err)
	}

	if retrieved.Token != stored.Token {
		t.Errorf("Token mismatch: expected %s, got %s", stored.Token, retrieved.Token)
	}

	if retrieved.First6 != stored.First6 {
		t.Errorf("First6 mismatch: expected %s, got %s", stored.First6, retrieved.First6)
	}
}

func TestDecryptCard(t *testing.T) {
	store, db := setupVaultStore(t)
	defer db.Close()

	ctx := context.Background()

	// Store a card
	stored, err := store.StoreCard(ctx, "4532015112830366", "123", "12/25", "John Doe")
	if err != nil {
		t.Fatalf("Failed to store card: %v", err)
	}

	// Decrypt the card
	pan, cvv, expiry, cardholder, err := store.DecryptCard(ctx, stored)
	if err != nil {
		t.Fatalf("Failed to decrypt card: %v", err)
	}

	if pan != "4532015112830366" {
		t.Errorf("PAN mismatch: expected 4532015112830366, got %s", pan)
	}

	if cvv != "123" {
		t.Errorf("CVV mismatch: expected 123, got %s", cvv)
	}

	if expiry != "12/25" {
		t.Errorf("Expiry mismatch: expected 12/25, got %s", expiry)
	}

	if cardholder != "John Doe" {
		t.Errorf("Cardholder mismatch: expected John Doe, got %s", cardholder)
	}
}

func TestStoreAndRetrieveFullWorkflow(t *testing.T) {
	store, db := setupVaultStore(t)
	defer db.Close()

	ctx := context.Background()

	// Store a card
	stored, err := store.StoreCard(ctx, "4532015112830366", "123", "12/25", "John Doe")
	if err != nil {
		t.Fatalf("Failed to store card: %v", err)
	}

	// Retrieve the card
	retrieved, err := store.RetrieveCard(ctx, stored.Token)
	if err != nil {
		t.Fatalf("Failed to retrieve card: %v", err)
	}

	// Decrypt the card
	pan, cvv, expiry, cardholder, err := store.DecryptCard(ctx, retrieved)
	if err != nil {
		t.Fatalf("Failed to decrypt card: %v", err)
	}

	// Verify all data
	if pan != "4532015112830366" || cvv != "123" || expiry != "12/25" || cardholder != "John Doe" {
		t.Error("Decrypted card data does not match original")
	}
}

func TestStoreCardInvalidData(t *testing.T) {
	store, db := setupVaultStore(t)
	defer db.Close()

	ctx := context.Background()

	testCases := []struct {
		pan        string
		cvv        string
		expiry     string
		cardholder string
		shouldFail bool
	}{
		{"1234567890123456", "123", "12/25", "John Doe", true},  // Invalid Luhn
		{"4532015112830366", "12", "12/25", "John Doe", true},   // Invalid CVV
		{"4532015112830366", "123", "13/25", "John Doe", true},  // Invalid expiry
		{"4532015112830366", "123", "12/25", "", true},          // Empty cardholder
	}

	for _, tc := range testCases {
		_, err := store.StoreCard(ctx, tc.pan, tc.cvv, tc.expiry, tc.cardholder)
		if tc.shouldFail && err == nil {
			t.Errorf("Expected error for invalid data: PAN=%s, CVV=%s, Expiry=%s, Cardholder=%s",
				tc.pan, tc.cvv, tc.expiry, tc.cardholder)
		}
	}
}

func TestRetrieveNonexistentCard(t *testing.T) {
	store, db := setupVaultStore(t)
	defer db.Close()

	ctx := context.Background()

	_, err := store.RetrieveCard(ctx, "nonexistent-token")
	if err == nil {
		t.Error("Expected error when retrieving nonexistent card")
	}
}

func TestMultipleCards(t *testing.T) {
	store, db := setupVaultStore(t)
	defer db.Close()

	ctx := context.Background()

	// Store multiple cards
	cards := []struct {
		pan   string
		cvv   string
		expiry string
		name  string
	}{
		{"4532015112830366", "123", "12/25", "John Doe"},
		{"5425233010103442", "456", "06/26", "Jane Smith"},
		{"378282246310005", "789", "09/27", "Bob Johnson"},
	}

	tokens := make([]string, len(cards))
	for i, card := range cards {
		result, err := store.StoreCard(ctx, card.pan, card.cvv, card.expiry, card.name)
		if err != nil {
			t.Fatalf("Failed to store card %d: %v", i, err)
		}
		tokens[i] = result.Token
	}

	// Verify each card can be retrieved and decrypted
	for i, card := range cards {
		retrieved, err := store.RetrieveCard(ctx, tokens[i])
		if err != nil {
			t.Fatalf("Failed to retrieve card %d: %v", i, err)
		}

		pan, cvv, expiry, name, err := store.DecryptCard(ctx, retrieved)
		if err != nil {
			t.Fatalf("Failed to decrypt card %d: %v", i, err)
		}

		if pan != card.pan || cvv != card.cvv || expiry != card.expiry || name != card.name {
			t.Errorf("Card %d data mismatch", i)
		}
	}
}
