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
//
// In production/staging environments, it requires secret indirection for sensitive
// values (e.g., DATABASE_URL must be a KMS/Vault reference).
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

// Load is kept for backward compatibility.
func Load() (*Config, error) {
    return LoadFromEnv()
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

    if len(missing) > 0 {
        return errors.New("missing required environment variables: " + strings.Join(missing, ", "))
    }

    // In development/testing environments, allow plain DATABASE_URL.
    // In production/staging environments, require secret indirection.
    if c.Environment == "production" || c.Environment == "staging" {
        if c.AuditSink == "" {
            missing = append(missing, "AUDIT_SINK")
        }
        if c.KMSSigner == "" {
            missing = append(missing, "KMS_SIGNER")
        }

        if len(missing) > 0 {
            return errors.New("missing required environment variables for " + c.Environment + ": " + strings.Join(missing, ", "))
        }

        if !isSecretReference(c.DatabaseURL) {
            return errors.New("DATABASE_URL must be a secret reference (start with aws-kms://, gcp-kms://, or vault://)")
        }

        if c.KMSSigner != "" && !isSecretReference(c.KMSSigner) {
            return errors.New("KMS_SIGNER must be a KMS reference (start with aws-kms://, gcp-kms://, or vault://)")
        }
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
