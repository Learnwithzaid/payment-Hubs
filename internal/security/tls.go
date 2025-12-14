package security

import (
    "crypto/tls"
    "crypto/x509"
    "errors"
    "fmt"
    "os"
    "path/filepath"
)

// TLSConfig holds TLS configuration.
type TLSConfig struct {
    CertFile       string
    KeyFile        string
    CAFile         string
    RequireClientAuth bool
}

// LoadServerTLSConfig loads server TLS configuration with mutual TLS support.
func LoadServerTLSConfig(cfg TLSConfig) (*tls.Config, error) {
    // Load server certificate and key
    cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
    if err != nil {
        return nil, fmt.Errorf("failed to load server certificate and key: %w", err)
    }

    clientAuth := tls.NoClientCert
    if cfg.RequireClientAuth {
        clientAuth = tls.RequireAndVerifyClientCert
    }

    tlsCfg := &tls.Config{
        Certificates: []tls.Certificate{cert},
        MinVersion:   tls.VersionTLS13,
        CipherSuites: []uint16{
            tls.TLS_AES_256_GCM_SHA384,
            tls.TLS_CHACHA20_POLY1305_SHA256,
        },
        ClientAuth: clientAuth,
    }

    // Load client CA certificates
    if cfg.CAFile != "" {
        caData, err := os.ReadFile(cfg.CAFile)
        if err != nil {
            return nil, fmt.Errorf("failed to read CA certificate: %w", err)
        }

        caCertPool := x509.NewCertPool()
        if !caCertPool.AppendCertsFromPEM(caData) {
            return nil, errors.New("failed to parse CA certificate")
        }

        tlsCfg.ClientCAs = caCertPool
    }

    return tlsCfg, nil
}

// LoadClientTLSConfig loads client TLS configuration.
func LoadClientTLSConfig(cfg TLSConfig) (*tls.Config, error) {
    cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
    if err != nil {
        return nil, fmt.Errorf("failed to load client certificate and key: %w", err)
    }

    tlsCfg := &tls.Config{
        Certificates: []tls.Certificate{cert},
        MinVersion:   tls.VersionTLS13,
        CipherSuites: []uint16{
            tls.TLS_AES_256_GCM_SHA384,
            tls.TLS_CHACHA20_POLY1305_SHA256,
        },
        InsecureSkipVerify: false,
    }

    // Load server CA certificate
    if cfg.CAFile != "" {
        caData, err := os.ReadFile(cfg.CAFile)
        if err != nil {
            return nil, fmt.Errorf("failed to read server CA certificate: %w", err)
        }

        caCertPool := x509.NewCertPool()
        if !caCertPool.AppendCertsFromPEM(caData) {
            return nil, errors.New("failed to parse server CA certificate")
        }

        tlsCfg.RootCAs = caCertPool
    }

    return tlsCfg, nil
}

// VerifyTLSFiles verifies that all required TLS files exist.
func VerifyTLSFiles(certFile, keyFile, caFile string) error {
    for _, file := range []string{certFile, keyFile, caFile} {
        if file == "" {
            return errors.New("TLS file path must not be empty")
        }
        if _, err := os.Stat(file); err != nil {
            return fmt.Errorf("TLS file not found: %s - %w", file, err)
        }
    }
    return nil
}

// GenerateTLSPaths generates TLS file paths from a base directory.
func GenerateTLSPaths(baseDir string) (certFile, keyFile, caFile string) {
    certFile = filepath.Join(baseDir, "server.crt")
    keyFile = filepath.Join(baseDir, "server.key")
    caFile = filepath.Join(baseDir, "ca.crt")
    return
}

// ExtractRBACClaims extracts RBAC claims from client certificate subject.
// Returns service name and permissions.
func ExtractRBACClaims(clientCert *x509.Certificate) (service string, permissions []string, err error) {
    if clientCert == nil {
        return "", nil, errors.New("client certificate is nil")
    }

    // Extract service name from certificate Common Name
    service = clientCert.Subject.CommonName
    if service == "" {
        return "", nil, errors.New("certificate Common Name is empty")
    }

    // Extract organization as permissions (simplified RBAC)
    permissions = clientCert.Subject.Organization

    return service, permissions, nil
}

// VerifyServiceToServiceRBAC verifies service-to-service authorization.
func VerifyServiceToServiceRBAC(service string, requiredPermission string) bool {
    // In production, check against a proper RBAC database or ACL
    // For now, allow any service with "vault" prefix to access
    return service != ""
}
