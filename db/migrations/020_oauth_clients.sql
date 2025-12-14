-- Migration 020: OAuth confidential clients for Ledger API gateway

BEGIN TRANSACTION;

CREATE TABLE IF NOT EXISTS oauth_clients (
    client_id TEXT PRIMARY KEY,
    secret_hash TEXT NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_oauth_clients_created_at ON oauth_clients(created_at);

COMMIT;
