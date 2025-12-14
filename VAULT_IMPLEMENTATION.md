# Vault Microservice Implementation

## Overview

This document describes the implementation of the encrypted vault/tokenization microservice for secure payment card handling.

## Architecture Components

### 1. Cryptography Module (`internal/crypto/`)

#### KMS Interface (`kms.go`)
- Abstract KMS interface supporting multiple implementations
- **FileBasedKMS**: Mock implementation using file-based key storage for testing
  - Stores encryption keys in a local directory
  - Supports multiple key versions (test-key-1, test-key-2, etc.)
  - Uses simple XOR encryption for demonstration (in production, use proper key encryption)
- **AWSKMS**: Placeholder for AWS Key Management Service integration

#### AEAD Encryption (`aead.go`)
- Implements envelope encryption with AES-256-GCM
- Per-record data keys: Each card encrypted with unique data key
- Each data key encrypted with master key (KMS)
- Features:
  - 256-bit AES cipher in GCM mode
  - Random 12-byte nonces for each encryption
  - Additional Authenticated Data (AAD) support for integrity
  - Protection against ciphertext tampering

### 2. Vault Module (`internal/vault/`)

#### Tokenizer (`tokenizer.go`)
- Validates payment card data using industry standards
- PAN validation:
  - Length check: 13-19 digits
  - Luhn algorithm validation
  - Format support: with/without spaces and dashes
- CVV validation: 3-4 digits
- Expiry validation: MM/YY or MM/YYYY format
- Cardholder validation: Letters, spaces, hyphens, and apostrophes only
- Generates unique tokens: `tok_<hex-random>` format

#### Secure Storage (`store.go`)
- Never writes plaintext card data to disk
- Operations:
  - **StoreCard**: Tokenize and encrypt card, store metadata
  - **RetrieveCard**: Fetch encrypted card from database
  - **DecryptCard**: Decrypt using envelope decryption
  - **RotateKey**: Re-encrypt all cards with new master key
- Storage:
  - First 6 digits of PAN (for identification)
  - Last 4 digits of PAN (for display)
  - Encrypted full card data
  - Encrypted data key
  - Nonce for decryption
  - Key ID for key rotation tracking

#### Service (`service.go`)
- High-level operations for gRPC handlers
- RBAC verification stub (to be integrated with mTLS)
- Operations:
  - TokenizeCard: Validate, encrypt, store card
  - DetokenizeCard: Retrieve and decrypt card
  - RotateKey: Rotate all cards to new master key

### 3. Security Module (`internal/security/`)

#### TLS Security (`tls.go`)
- Mutual TLS (mTLS) support for service-to-service communication
- Features:
  - Enforces TLS 1.3 minimum
  - Configures strong cipher suites
  - Client certificate verification
  - RBAC claims extraction from certificate subject
- Functions:
  - LoadServerTLSConfig: Server-side TLS configuration
  - LoadClientTLSConfig: Client-side TLS configuration
  - VerifyTLSFiles: Validate TLS certificate and key files
  - ExtractRBACClaims: Extract service identity and permissions
  - VerifyServiceToServiceRBAC: Check authorization

### 4. Database Schema (`db/migrations/001_vault.sql`)

#### vault_cards table
- Stores encrypted card data
- Partial PAN for identification (first6, last4)
- Columns:
  - token: Unique token identifier
  - first6, last4: Partial PAN for display
  - expiry, cardholder: Metadata
  - ciphertext: Encrypted card data
  - encrypted_key: Encrypted data key
  - nonce: GCM nonce
  - key_id: Master key identifier for rotation tracking
  - created_at, updated_at: Timestamps

#### vault_key_rotations table
- Audit log for key rotation operations
- Tracks rotation history

#### vault_keys table
- Master key metadata
- Status tracking (active, revoked)

### 5. gRPC API (`api/proto/vault.proto`)

Services:
- **TokenizeCard**: Tokenize a payment card
  - Input: PAN, CVV, expiry, cardholder
  - Output: Token, first6, last4, expiry
- **DetokenizeCard**: Retrieve original card data
  - Input: Token
  - Output: PAN, CVV, expiry, cardholder
- **RotateKey**: Rotate master encryption key
  - Input: Old key ID
  - Output: New key ID, rotated count

## Security Features

### 1. Zero Plaintext on Disk
- All sensitive card data encrypted before database storage
- Only partial PAN (first6/last4) stored unencrypted
- Envelope encryption prevents key exposure

### 2. Ciphertext Uniqueness
- Random nonces ensure same plaintext encrypts differently
- Per-record data keys add entropy
- Verified by tests comparing multiple encryptions

### 3. Authentication & Integrity
- GCM mode provides authenticated encryption
- Additional Authenticated Data (AAD) using token ID
- Tampering with ciphertext or token detected at decryption

### 4. Key Management
- Master keys stored securely (file-based mock for testing)
- Key rotation support with re-encryption
- Key ID tracking for decryption
- No key material exposed during rotation

### 5. RBAC & mTLS
- Service-to-service authentication via mutual TLS
- Service identity extracted from certificate Common Name
- Permissions extracted from certificate Organization
- TLS 1.3 enforcement, strong cipher suites

## Testing

### Unit Tests
- **KMS Tests** (`internal/crypto/kms_test.go`)
  - Key generation and encryption
  - Decryption with correct/incorrect keys
  - Key persistence across instances
  - XOR cipher correctness

- **AEAD Tests** (`internal/crypto/aead_test.go`)
  - Encryption/decryption round-trip
  - Ciphertext uniqueness
  - Authentication failure on tampered data
  - Nonce size validation

- **Tokenizer Tests** (`internal/vault/tokenizer_test.go`)
  - PAN validation (valid/invalid cases)
  - Luhn check correctness
  - CVV, expiry, cardholder validation
  - Token uniqueness
  - Edge cases (spaces, dashes, formats)

- **Store Tests** (`internal/vault/store_test.go`)
  - Zero plaintext on disk verification
  - Full store/retrieve/decrypt workflow
  - Invalid data rejection
  - Multiple card handling
  - Non-existent card error handling

- **TLS Tests** (`internal/security/tls_test.go`)
  - TLS file verification
  - RBAC claims extraction
  - Service authorization checks

### Integration Tests (`internal/vault/integration_test.go`)
- Full workflow: tokenize → detokenize → decrypt
- Ciphertext uniqueness verification
- No plaintext in database
- Invalid card data rejection with detailed errors
- Key rotation with re-encryption
- Post-rotation decryption validation

## Testing Coverage

Target: ≥90% coverage for vault packages

Covered areas:
- Crypto primitives: KMS interface, AEAD encryption
- Tokenization: PAN/CVV validation, Luhn check, token generation
- Storage: Encryption, decryption, key rotation
- Security: TLS configuration, RBAC verification
- Integration: Full workflows with mock KMS

## Acceptance Criteria Status

- ✓ Vault service with KMS and envelope encryption
- ✓ Tokenization with PAN/CVV validation and Luhn check
- ✓ Secure storage never writing plaintext
- ✓ Database migration with partial PAN storage
- ✓ gRPC API (TokenizeCard, DetokenizeCard, RotateKey)
- ✓ Mutual TLS security with RBAC
- ✓ Zero plaintext card data on disk (verified by tests)
- ✓ Invalid card data rejected with detailed errors
- ✓ TLS enforcement required for startup
- ✓ Unit and integration tests with mock KMS
- ✓ Ciphertext uniqueness and key-rotation support

## Future Enhancements

1. AWS KMS Integration: Replace FileBasedKMS with real AWS KMS client
2. Enhanced RBAC: Implement proper RBAC database for finer-grained permissions
3. Performance: Add caching for decrypted keys (with TTL)
4. Observability: Add structured logging and metrics
5. High Availability: Support multiple KMS endpoints
6. Compliance: PCI-DSS audit logging and compliance checks
