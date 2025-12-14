package config

import (
	"errors"
	"os"
	"strings"
)

// Config holds the application configuration.
type Config struct {
	Environment string
	DatabaseURL string
	AuditSink   string
	KMSSigner   string
}

// LoadFromEnv loads configuration from environment variables.
// It performs strict validation and checks for secret indirection.
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		Environment: os.Getenv("APP_ENV"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		AuditSink:   os.Getenv("AUDIT_SINK"),
		KMSSigner:   os.Getenv("KMS_SIGNER"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	var missing []string

	if c.Environment == "" {
		missing = append(missing, "APP_ENV")
	}
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.AuditSink == "" {
		missing = append(missing, "AUDIT_SINK")
	}
	if c.KMSSigner == "" {
		missing = append(missing, "KMS_SIGNER")
	}

	if len(missing) > 0 {
		return errors.New("missing required environment variables: " + strings.Join(missing, ", "))
	}

	// Secret indirection check: key fields should use KMS references
	if !isSecretReference(c.DatabaseURL) {
		return errors.New("DATABASE_URL must be a KMS reference (start with aws-kms://, gcp-kms://, or vault://)")
	}
	// AuditSink might not be a secret (e.g. log group name), but KMSSigner definitely is a key reference
	if !isSecretReference(c.KMSSigner) {
		return errors.New("KMS_SIGNER must be a KMS reference (start with aws-kms://, gcp-kms://, or vault://)")
	}

	return nil
}

func isSecretReference(val string) bool {
	prefixes := []string{"aws-kms://", "gcp-kms://", "vault://"}
	for _, p := range prefixes {
		if strings.HasPrefix(val, p) {
			return true
		}
	}
	return false
}
