package config

import (
	"os"
	"testing"
)

func TestLoadFromEnv(t *testing.T) {
	// Helper to reset env
	resetEnv := func() {
		os.Unsetenv("APP_ENV")
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("AUDIT_SINK")
		os.Unsetenv("KMS_SIGNER")
	}
	resetEnv()
	defer resetEnv()

	// 1. Missing secrets -> Fail
	_, err := LoadFromEnv()
	if err == nil {
		t.Error("expected error when env vars are missing, got nil")
	}

	// 2. Partial env -> Fail
	os.Setenv("APP_ENV", "production")
	_, err = LoadFromEnv()
	if err == nil {
		t.Error("expected error when some env vars are missing, got nil")
	}

	// 3. Invalid secret format (DATABASE_URL) -> Fail
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db") // Plaintext
	os.Setenv("AUDIT_SINK", "cloudwatch://logs")
	os.Setenv("KMS_SIGNER", "aws-kms://key-id")

	_, err = LoadFromEnv()
	if err == nil {
		t.Error("expected error when DATABASE_URL is not a KMS reference")
	}

	// 4. Valid config -> Success
	os.Setenv("DATABASE_URL", "aws-kms://ciphertext-blob")
	config, err := LoadFromEnv()
	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}
	if config.Environment != "production" {
		t.Errorf("expected Environment=production, got %s", config.Environment)
	}
}
