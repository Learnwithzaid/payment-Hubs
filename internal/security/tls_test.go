package security

import (
    "crypto/rand"
    "crypto/rsa"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/pem"
    "math/big"
    "os"
    "testing"
    "time"
)

func generateSelfSignedCert(t *testing.T, commonName string) (certFile, keyFile string) {
    // Generate private key
    privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        t.Fatalf("Failed to generate private key: %v", err)
    }

    // Create certificate template
    template := x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject: pkix.Name{
            CommonName: commonName,
        },
        NotBefore: time.Now(),
        NotAfter:  time.Now().Add(time.Hour),
        KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
    }

    // Create self-signed certificate
    certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
    if err != nil {
        t.Fatalf("Failed to create certificate: %v", err)
    }

    // Write certificate to file
    tmpDir := t.TempDir()
    certFile = tmpDir + "/test.crt"
    keyFile = tmpDir + "/test.key"

    // Write certificate as PEM
    certPEM := pem.EncodeToMemory(&pem.Block{
        Type:  "CERTIFICATE",
        Bytes: certDER,
    })
    if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
        t.Fatalf("Failed to write certificate: %v", err)
    }

    // Write private key as PEM
    keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
    if err != nil {
        t.Fatalf("Failed to marshal private key: %v", err)
    }
    keyPEM := pem.EncodeToMemory(&pem.Block{
        Type:  "PRIVATE KEY",
        Bytes: keyDER,
    })
    if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
        t.Fatalf("Failed to write key: %v", err)
    }

    return certFile, keyFile
}

func TestVerifyTLSFilesExists(t *testing.T) {
    certFile, keyFile := generateSelfSignedCert(t, "test")
    tmpDir := t.TempDir()
    caFile := tmpDir + "/ca.crt"

    // Create empty CA file
    if err := os.WriteFile(caFile, []byte("test"), 0600); err != nil {
        t.Fatalf("Failed to create CA file: %v", err)
    }

    err := VerifyTLSFiles(certFile, keyFile, caFile)
    if err != nil {
        t.Errorf("VerifyTLSFiles should not fail with existing files: %v", err)
    }
}

func TestVerifyTLSFilesMissing(t *testing.T) {
    err := VerifyTLSFiles("/nonexistent/cert.crt", "/nonexistent/key.key", "/nonexistent/ca.crt")
    if err == nil {
        t.Error("VerifyTLSFiles should fail with missing files")
    }
}

func TestVerifyTLSFilesEmpty(t *testing.T) {
    err := VerifyTLSFiles("", "", "")
    if err == nil {
        t.Error("VerifyTLSFiles should fail with empty paths")
    }
}

func TestGenerateTLSPaths(t *testing.T) {
    baseDir := "/var/lib/vault"
    certFile, keyFile, caFile := GenerateTLSPaths(baseDir)

    if certFile != "/var/lib/vault/server.crt" {
        t.Errorf("Expected cert file path /var/lib/vault/server.crt, got %s", certFile)
    }

    if keyFile != "/var/lib/vault/server.key" {
        t.Errorf("Expected key file path /var/lib/vault/server.key, got %s", keyFile)
    }

    if caFile != "/var/lib/vault/ca.crt" {
        t.Errorf("Expected CA file path /var/lib/vault/ca.crt, got %s", caFile)
    }
}

func TestExtractRBACClaimsValid(t *testing.T) {
    cert := &x509.Certificate{
        Subject: pkix.Name{
            CommonName:   "vault-service",
            Organization: []string{"vault", "admin"},
        },
    }

    service, permissions, err := ExtractRBACClaims(cert)
    if err != nil {
        t.Fatalf("ExtractRBACClaims failed: %v", err)
    }

    if service != "vault-service" {
        t.Errorf("Expected service 'vault-service', got '%s'", service)
    }

    if len(permissions) != 2 {
        t.Errorf("Expected 2 permissions, got %d", len(permissions))
    }
}

func TestExtractRBACClaimsNilCertificate(t *testing.T) {
    _, _, err := ExtractRBACClaims(nil)
    if err == nil {
        t.Error("ExtractRBACClaims should fail with nil certificate")
    }
}

func TestExtractRBACClaimsEmptyCommonName(t *testing.T) {
    cert := &x509.Certificate{
        Subject: pkix.Name{
            CommonName: "",
        },
    }

    _, _, err := ExtractRBACClaims(cert)
    if err == nil {
        t.Error("ExtractRBACClaims should fail with empty common name")
    }
}

func TestVerifyServiceToServiceRBAC(t *testing.T) {
    // Any non-empty service should be allowed
    if !VerifyServiceToServiceRBAC("vault-service", "tokenize") {
        t.Error("Service should be authorized")
    }

    if !VerifyServiceToServiceRBAC("api-service", "tokenize") {
        t.Error("Service should be authorized")
    }

    // Empty service should not be authorized
    if VerifyServiceToServiceRBAC("", "tokenize") {
        t.Error("Empty service should not be authorized")
    }
}
