package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Client struct {
	ID         string
	SecretHash string
	Scopes     []string
}

type ClientStore interface {
	GetClient(ctx context.Context, clientID string) (*Client, error)
}

type OAuthServer struct {
	Store          ClientStore
	Keys           *KeySet
	Issuer         string
	AccessTokenTTL time.Duration
}

type AccessTokenClaims struct {
	jwt.RegisteredClaims
	ClientID string   `json:"client_id"`
	Scopes   []string `json:"scopes"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
}

func HashClientSecret(secret string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func VerifyClientSecret(hash, secret string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret)) == nil
}

func (s *OAuthServer) TokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	_ = r.ParseForm()
	grantType := r.FormValue("grant_type")
	if grantType == "" {
		grantType = r.URL.Query().Get("grant_type")
	}

	if grantType != "client_credentials" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}

	clientID, clientSecret, ok := r.BasicAuth()
	if !ok {
		clientID = r.FormValue("client_id")
		clientSecret = r.FormValue("client_secret")
	}

	if clientID == "" || clientSecret == "" {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}

	client, err := s.Store.GetClient(r.Context(), clientID)
	if err != nil || client == nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}

	if !VerifyClientSecret(client.SecretHash, clientSecret) {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}

	reqScope := strings.TrimSpace(r.FormValue("scope"))
	var requested []string
	if reqScope != "" {
		requested = strings.Fields(reqScope)
	}

	granted := intersectScopes(client.Scopes, requested)
	if len(requested) > 0 && len(granted) == 0 {
		writeOAuthError(w, http.StatusForbidden, "invalid_scope")
		return
	}

	exp := s.AccessTokenTTL
	if exp == 0 {
		exp = 15 * time.Minute
	}

	claims := AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.Issuer,
			Subject:   client.ID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(exp)),
			ID:        uuid.NewString(),
		},
		ClientID: client.ID,
		Scopes:   granted,
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = s.Keys.KeyID()

	signed, err := tok.SignedString(s.Keys.PrivateKey())
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TokenResponse{
		AccessToken: signed,
		TokenType:   "Bearer",
		ExpiresIn:   int64(exp.Seconds()),
		Scope:       strings.Join(granted, " "),
	})
}

func (s *OAuthServer) JWKSHandler(w http.ResponseWriter, r *http.Request) {
	jwks, err := s.Keys.JWKS()
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jwks)
}

func writeOAuthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func intersectScopes(allowed []string, requested []string) []string {
	allowedSet := map[string]struct{}{}
	for _, s := range allowed {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		allowedSet[s] = struct{}{}
	}

	if len(requested) == 0 {
		out := make([]string, 0, len(allowedSet))
		for s := range allowedSet {
			out = append(out, s)
		}
		sortStrings(out)
		return out
	}

	var out []string
	for _, s := range requested {
		if _, ok := allowedSet[s]; ok {
			out = append(out, s)
		}
	}
	return out
}

func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

var ErrClientNotFound = errors.New("client not found")
