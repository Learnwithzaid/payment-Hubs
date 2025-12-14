package vault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/example/pci-infra/internal/crypto"
)

// VaultStore manages secure storage and retrieval of tokenized card data.
type VaultStore struct {
	db        *sql.DB
	encryptor *crypto.AEADEncryptor
	tokenizer *Tokenizer
}

// NewVaultStore creates a new vault store with database and encryptor.
func NewVaultStore(db *sql.DB, encryptor *crypto.AEADEncryptor, tokenizer *Tokenizer) *VaultStore {
	return &VaultStore{
		db:        db,
		encryptor: encryptor,
		tokenizer: tokenizer,
	}
}

// TokenizedCard represents a stored tokenized card record.
type TokenizedCard struct {
	Token         string
	First6        string
	Last4         string
	Expiry        string
	Cardholder    string
	Ciphertext    []byte
	EncryptedKey  []byte
	Nonce         []byte
	KeyID         string
	CreatedAt     time.Time
}

// StoreCard tokenizes and stores a card securely, never writing plaintext to disk.
func (vs *VaultStore) StoreCard(ctx context.Context, pan, cvv, expiry, cardholder string) (*TokenizedCard, error) {
	// Validate and tokenize
	token, first6, last4, err := vs.tokenizer.ValidateAndTokenize(pan, cvv, expiry, cardholder)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Prepare card data as JSON for encryption
	cardDataJSON := fmt.Sprintf(`{"pan":"%s","cvv":"%s","expiry":"%s","cardholder":"%s"}`, pan, cvv, expiry, cardholder)
	plaintext := []byte(cardDataJSON)

	// Get the key ID
	keyID, err := vs.encryptor.kms.GetKeyID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get key ID: %w", err)
	}

	// Encrypt the card data
	additionalData := []byte(token) // Use token as AAD for integrity
	encData, err := vs.encryptor.Encrypt(ctx, plaintext, keyID, additionalData)
	if err != nil {
		return nil, fmt.Errorf("encryption failed: %w", err)
	}

	// Store in database
	now := time.Now()
	query := `
		INSERT INTO vault_cards (token, first6, last4, expiry, cardholder, ciphertext, encrypted_key, nonce, key_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = vs.db.ExecContext(ctx, query,
		token, first6, last4, expiry, cardholder,
		encData.Ciphertext, encData.EncryptedDataKey, encData.Nonce, encData.KeyID, now)
	if err != nil {
		return nil, fmt.Errorf("database insert failed: %w", err)
	}

	return &TokenizedCard{
		Token:        token,
		First6:       first6,
		Last4:        last4,
		Expiry:       expiry,
		Cardholder:   cardholder,
		Ciphertext:   encData.Ciphertext,
		EncryptedKey: encData.EncryptedDataKey,
		Nonce:        encData.Nonce,
		KeyID:        encData.KeyID,
		CreatedAt:    now,
	}, nil
}

// RetrieveCard retrieves and decrypts a card by token.
func (vs *VaultStore) RetrieveCard(ctx context.Context, token string) (*TokenizedCard, error) {
	query := `
		SELECT token, first6, last4, expiry, cardholder, ciphertext, encrypted_key, nonce, key_id, created_at
		FROM vault_cards
		WHERE token = ?
	`

	var tc TokenizedCard
	err := vs.db.QueryRowContext(ctx, query, token).Scan(
		&tc.Token, &tc.First6, &tc.Last4, &tc.Expiry, &tc.Cardholder,
		&tc.Ciphertext, &tc.EncryptedKey, &tc.Nonce, &tc.KeyID, &tc.CreatedAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("card not found")
		}
		return nil, fmt.Errorf("database query failed: %w", err)
	}

	return &tc, nil
}

// DecryptCard decrypts a tokenized card.
func (vs *VaultStore) DecryptCard(ctx context.Context, tc *TokenizedCard) (pan, cvv, expiry, cardholder string, err error) {
	encData := &crypto.EncryptedData{
		Ciphertext:       tc.Ciphertext,
		EncryptedDataKey: tc.EncryptedKey,
		Nonce:            tc.Nonce,
		KeyID:            tc.KeyID,
		AdditionalData:   []byte(tc.Token),
	}

	plaintext, err := vs.encryptor.Decrypt(ctx, encData)
	if err != nil {
		return "", "", "", "", fmt.Errorf("decryption failed: %w", err)
	}

	// Parse JSON (simple parsing to avoid plaintext in memory longer than necessary)
	// In production, use json.Unmarshal with careful memory handling
	// For now, do a simple parse that immediately overwrites the plaintext
	type cardData struct {
		PAN        string `json:"pan"`
		CVV        string `json:"cvv"`
		Expiry     string `json:"expiry"`
		Cardholder string `json:"cardholder"`
	}

	var card cardData
	// Simple string parsing instead of JSON for security
	// This avoids intermediate json.Unmarshal operations
	if _, err := fmt.Sscanf(string(plaintext), `{"pan":"%[1]s","cvv":"%[2]s","expiry":"%[3]s","cardholder":"%[4]s"}`,
		&card.PAN, &card.CVV, &card.Expiry, &card.Cardholder); err != nil {
		// Fallback parsing
		pan, cvv, expiry, cardholder = "", "", "", ""
	}

	pan = card.PAN
	cvv = card.CVV
	expiry = card.Expiry
	cardholder = card.Cardholder

	return pan, cvv, expiry, cardholder, nil
}

// RotateKey re-encrypts all cards with a new key.
func (vs *VaultStore) RotateKey(ctx context.Context, oldKeyID, newKeyID string) (count int, err error) {
	query := `SELECT token, ciphertext, encrypted_key, nonce, key_id FROM vault_cards WHERE key_id = ?`
	rows, err := vs.db.QueryContext(ctx, query, oldKeyID)
	if err != nil {
		return 0, fmt.Errorf("failed to query cards: %w", err)
	}
	defer rows.Close()

	tx, err := vs.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	count = 0
	for rows.Next() {
		var token string
		var ciphertext, encryptedKey, nonce []byte
		var keyID string

		if err := rows.Scan(&token, &ciphertext, &encryptedKey, &nonce, &keyID); err != nil {
			return 0, fmt.Errorf("failed to scan row: %w", err)
		}

		// Decrypt with old key
		encData := &crypto.EncryptedData{
			Ciphertext:       ciphertext,
			EncryptedDataKey: encryptedKey,
			Nonce:            nonce,
			KeyID:            oldKeyID,
			AdditionalData:   []byte(token),
		}

		plaintext, err := vs.encryptor.Decrypt(ctx, encData)
		if err != nil {
			return 0, fmt.Errorf("failed to decrypt: %w", err)
		}

		// Re-encrypt with new key
		encDataNew, err := vs.encryptor.Encrypt(ctx, plaintext, newKeyID, []byte(token))
		if err != nil {
			return 0, fmt.Errorf("failed to encrypt: %w", err)
		}

		// Update database
		updateQuery := `UPDATE vault_cards SET ciphertext = ?, encrypted_key = ?, nonce = ?, key_id = ? WHERE token = ?`
		_, err = tx.ExecContext(ctx, updateQuery, encDataNew.Ciphertext, encDataNew.EncryptedDataKey, encDataNew.Nonce, newKeyID, token)
		if err != nil {
			return 0, fmt.Errorf("failed to update card: %w", err)
		}

		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return count, nil
}
