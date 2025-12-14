-- Vault migration: Create tables for storing tokenized card data
-- This migration creates secure storage for encrypted card data with never writing plaintext

BEGIN TRANSACTION;

-- Create vault_cards table for storing encrypted card data
CREATE TABLE IF NOT EXISTS vault_cards (
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

-- Create indices for efficient lookups
CREATE INDEX idx_vault_cards_token ON vault_cards(token);
CREATE INDEX idx_vault_cards_first6_last4 ON vault_cards(first6, last4);
CREATE INDEX idx_vault_cards_key_id ON vault_cards(key_id);
CREATE INDEX idx_vault_cards_created_at ON vault_cards(created_at);

-- Create audit log table for key rotations
CREATE TABLE IF NOT EXISTS vault_key_rotations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    old_key_id TEXT NOT NULL,
    new_key_id TEXT NOT NULL,
    rotated_count INTEGER NOT NULL,
    rotated_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_vault_key_rotations_timestamp ON vault_key_rotations(rotated_at);

-- Create table to track encryption keys and their metadata
CREATE TABLE IF NOT EXISTS vault_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key_id TEXT UNIQUE NOT NULL,
    created_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP,
    status TEXT DEFAULT 'active'
);

CREATE INDEX idx_vault_keys_status ON vault_keys(status);

COMMIT;
