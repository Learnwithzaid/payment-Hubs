package vault

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/example/pci-infra/internal/security"
)

// VaultService implements the gRPC vault service.
type VaultService struct {
	store *VaultStore
}

// NewVaultService creates a new vault service.
func NewVaultService(store *VaultStore) *VaultService {
	return &VaultService{
		store: store,
	}
}

// TokenizeCard tokenizes a payment card.
// This method would be called from the gRPC handler after TLS verification.
func (vs *VaultService) TokenizeCard(ctx context.Context, pan, cvv, expiry, cardholder string) (token, first6, last4 string, err error) {
	// Verify RBAC claims from context (would be extracted from mTLS certificate)
	if _, err := extractServiceClaims(ctx); err != nil {
		return "", "", "", fmt.Errorf("RBAC verification failed: %w", err)
	}

	// Store the card
	result, err := vs.store.StoreCard(ctx, pan, cvv, expiry, cardholder)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to tokenize card: %w", err)
	}

	return result.Token, result.First6, result.Last4, nil
}

// DetokenizeCard detokenizes a payment card by token.
func (vs *VaultService) DetokenizeCard(ctx context.Context, token string) (pan, cvv, expiry, cardholder string, err error) {
	// Verify RBAC claims from context
	if _, err := extractServiceClaims(ctx); err != nil {
		return "", "", "", "", fmt.Errorf("RBAC verification failed: %w", err)
	}

	// Retrieve the card
	card, err := vs.store.RetrieveCard(ctx, token)
	if err != nil {
		// Return non-sensitive error message
		return "", "", "", "", errors.New("card not found or decryption failed")
	}

	// Decrypt the card
	pan, cvv, expiry, cardholder, err := vs.store.DecryptCard(ctx, card)
	if err != nil {
		return "", "", "", "", errors.New("card decryption failed")
	}

	return pan, cvv, expiry, cardholder, nil
}

// RotateKey rotates encryption keys for all cards.
func (vs *VaultService) RotateKey(ctx context.Context, oldKeyID, newKeyID string) (count int, err error) {
	// Verify RBAC claims from context
	service, err := extractServiceClaims(ctx)
	if err != nil {
		return 0, fmt.Errorf("RBAC verification failed: %w", err)
	}

	// Only allow admin operations
	if !isAdminService(service) {
		return 0, errors.New("insufficient permissions for key rotation")
	}

	// Perform key rotation
	count, err = vs.store.RotateKey(ctx, oldKeyID, newKeyID)
	if err != nil {
		return 0, fmt.Errorf("key rotation failed: %w", err)
	}

	return count, nil
}

// extractServiceClaims extracts the service name and permissions from the context.
// In production, this would extract claims from the mTLS client certificate.
func extractServiceClaims(ctx context.Context) (string, error) {
	// In a real implementation, this would extract from:
	// 1. gRPC peer info
	// 2. Client certificate from TLS handshake
	// 3. Extract CN and organization claims

	// For now, return a placeholder
	// In production, this should verify actual TLS certificates
	service := ctx.Value("service")
	if service == nil {
		return "", errors.New("service identity not found in context")
	}

	serviceStr, ok := service.(string)
	if !ok || serviceStr == "" {
		return "", errors.New("invalid service identity")
	}

	return serviceStr, nil
}

// isAdminService checks if the service has admin privileges.
func isAdminService(service string) bool {
	// In production, check against an actual RBAC database
	// For now, allow vault-admin service
	return service == "vault-admin"
}

// VerifyClientCertificate verifies the client certificate from the TLS connection.
// This would be called by the gRPC interceptor.
func VerifyClientCertificate(clientCert *x509.Certificate) (service string, err error) {
	if clientCert == nil {
		return "", errors.New("client certificate is required")
	}

	service, permissions, err := security.ExtractRBACClaims(clientCert)
	if err != nil {
		return "", fmt.Errorf("failed to extract RBAC claims: %w", err)
	}

	if !security.VerifyServiceToServiceRBAC(service, "vault") {
		return "", errors.New("service not authorized for vault access")
	}

	_ = permissions // permissions would be used for additional checks
	return service, nil
}
