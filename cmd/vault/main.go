package main

import (
    "database/sql"
    "fmt"
    "log"
    "net"
    "os"
    "os/signal"
    "syscall"

    _ "github.com/mattn/go-sqlite3"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials"

    "github.com/example/pci-infra/internal/crypto"
    "github.com/example/pci-infra/internal/security"
    "github.com/example/pci-infra/internal/vault"
)

func main() {
    // Verify required environment variables for vault service
    requiredEnv := []string{"APP_ENV", "VAULT_TLS_CERT", "VAULT_TLS_KEY", "VAULT_TLS_CA"}
    for _, env := range requiredEnv {
        if os.Getenv(env) == "" {
            log.Fatalf("Required environment variable not set: %s", env)
        }
    }

    // Initialize KMS (file-based for testing)
    keyStorePath := os.Getenv("KMS_KEY_STORE")
    if keyStorePath == "" {
        keyStorePath = "/tmp/vault-keys"
    }
    kmsConfig := crypto.FileBasedKMSConfig{
        KeyStorePath: keyStorePath,
    }
    kms, err := crypto.NewFileBasedKMS(kmsConfig)
    if err != nil {
        log.Fatalf("Failed to initialize KMS: %v", err)
    }

    // Initialize AEAD encryptor
    encryptor := crypto.NewAEADEncryptor(kms)

    // Initialize database
    db, err := initializeDatabase()
    if err != nil {
        log.Fatalf("Failed to initialize database: %v", err)
    }
    defer db.Close()

    // Initialize vault components
    tokenizer := vault.NewTokenizer()
    store := vault.NewVaultStore(db, encryptor, tokenizer)

    // Load TLS certificates
    tlsConfig := security.TLSConfig{
        CertFile:         os.Getenv("VAULT_TLS_CERT"),
        KeyFile:          os.Getenv("VAULT_TLS_KEY"),
        CAFile:           os.Getenv("VAULT_TLS_CA"),
        RequireClientAuth: true,
    }

    // Verify TLS files exist
    if err := security.VerifyTLSFiles(tlsConfig.CertFile, tlsConfig.KeyFile, tlsConfig.CAFile); err != nil {
        log.Fatalf("TLS verification failed: %v", err)
    }

    // Load TLS configuration
    serverTLSConfig, err := security.LoadServerTLSConfig(tlsConfig)
    if err != nil {
        log.Fatalf("Failed to load TLS configuration: %v", err)
    }

    // Create gRPC server with TLS
    tlsCreds := credentials.NewTLS(serverTLSConfig)
    grpcServer := grpc.NewServer(grpc.Creds(tlsCreds))

    // TODO: Register vault service when gRPC service is fully implemented
    // vaultpb.RegisterVaultServer(grpcServer, vault.NewVaultService(store))
    _ = store // Used for service registration

    // Start listening
    listener, err := net.Listen("tcp", ":50051")
    if err != nil {
        log.Fatalf("Failed to listen: %v", err)
    }
    defer listener.Close()

    log.Println("Vault service starting on :50051 with mutual TLS")

    // Handle graceful shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

    go func() {
        sig := <-sigChan
        log.Printf("Received signal: %v", sig)
        grpcServer.GracefulStop()
    }()

    // Serve
    if err := grpcServer.Serve(listener); err != nil {
        log.Fatalf("Failed to serve: %v", err)
    }
}

// initializeDatabase initializes the database and runs migrations.
func initializeDatabase() (*sql.DB, error) {
    // For now, use SQLite with a simple file path
    // In production, parse the proper database URL
    dbFile := os.Getenv("VAULT_DB_PATH")
    if dbFile == "" {
        dbFile = "vault.db"
    }

    db, err := sql.Open("sqlite3", dbFile)
    if err != nil {
        return nil, fmt.Errorf("failed to open database: %w", err)
    }

    // Test the connection
    if err := db.Ping(); err != nil {
        return nil, fmt.Errorf("failed to ping database: %w", err)
    }

    // Run migrations (simplified - in production use a migration tool)
    if err := runMigrations(db); err != nil {
        return nil, fmt.Errorf("failed to run migrations: %w", err)
    }

    return db, nil
}

// runMigrations runs the vault migrations.
func runMigrations(db *sql.DB) error {
    migrationSQL := `
    BEGIN TRANSACTION;

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

    CREATE INDEX IF NOT EXISTS idx_vault_cards_token ON vault_cards(token);
    CREATE INDEX IF NOT EXISTS idx_vault_cards_first6_last4 ON vault_cards(first6, last4);
    CREATE INDEX IF NOT EXISTS idx_vault_cards_key_id ON vault_cards(key_id);
    CREATE INDEX IF NOT EXISTS idx_vault_cards_created_at ON vault_cards(created_at);

    CREATE TABLE IF NOT EXISTS vault_key_rotations (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        old_key_id TEXT NOT NULL,
        new_key_id TEXT NOT NULL,
        rotated_count INTEGER NOT NULL,
        rotated_at TIMESTAMP NOT NULL
    );

    CREATE INDEX IF NOT EXISTS idx_vault_key_rotations_timestamp ON vault_key_rotations(rotated_at);

    CREATE TABLE IF NOT EXISTS vault_keys (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        key_id TEXT UNIQUE NOT NULL,
        created_at TIMESTAMP NOT NULL,
        revoked_at TIMESTAMP,
        status TEXT DEFAULT 'active'
    );

    CREATE INDEX IF NOT EXISTS idx_vault_keys_status ON vault_keys(status);

    COMMIT;
    `

    _, err := db.Exec(migrationSQL)
    return err
}
