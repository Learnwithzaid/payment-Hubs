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

// Load loads configuration from environment variables with flexible validation.
// For development/testing, it allows plain DATABASE_URL values.
func Load() (*Config, error) {
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

    if len(missing) > 0 {
        return errors.New("missing required environment variables: " + strings.Join(missing, ", "))
    }

    // In development/testing environments, allow plain DATABASE_URL
    // In production, require KMS references for sensitive data
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

        // Secret indirection check: key fields should use KMS references in production
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
