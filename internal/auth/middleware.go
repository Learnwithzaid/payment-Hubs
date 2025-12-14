package auth

import (
    "context"
    "errors"
    "net/http"
    "strings"

    "github.com/golang-jwt/jwt/v5"
)

type authInfoKey struct{}

type AuthInfo struct {
    ClientID string
    Scopes   map[string]struct{}
}

func AuthInfoFromContext(ctx context.Context) (*AuthInfo, bool) {
    v := ctx.Value(authInfoKey{})
    ai, ok := v.(*AuthInfo)
    return ai, ok
}

type JWTValidator struct {
    KeySet *KeySet
    Issuer string
}

func (v *JWTValidator) Validate(tokenString string) (*AccessTokenClaims, error) {
    if v.KeySet == nil || v.KeySet.PublicKey() == nil {
        return nil, errors.New("missing keyset")
    }

    claims := &AccessTokenClaims{}
    tok, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
        if t.Method.Alg() != jwt.SigningMethodRS256.Alg() {
            return nil, errors.New("unexpected signing method")
        }
        return v.KeySet.PublicKey(), nil
    }, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}))
    if err != nil {
        return nil, err
    }
    if !tok.Valid {
        return nil, errors.New("invalid token")
    }
    if v.Issuer != "" && claims.Issuer != v.Issuer {
        return nil, errors.New("invalid issuer")
    }
    return claims, nil
}

func Authenticate(v *JWTValidator, onError func(http.ResponseWriter, *http.Request, int, string)) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if v == nil {
                onError(w, r, http.StatusUnauthorized, "unauthorized")
                return
            }

            authz := r.Header.Get("Authorization")
            if authz == "" || !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
                onError(w, r, http.StatusUnauthorized, "unauthorized")
                return
            }

            tok := strings.TrimSpace(authz[len("Bearer "):])
            claims, err := v.Validate(tok)
            if err != nil {
                onError(w, r, http.StatusUnauthorized, "unauthorized")
                return
            }

            scopes := map[string]struct{}{}
            for _, s := range claims.Scopes {
                scopes[s] = struct{}{}
            }

            ai := &AuthInfo{ClientID: claims.ClientID, Scopes: scopes}
            ctx := context.WithValue(r.Context(), authInfoKey{}, ai)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

func RequireScopes(required ...string, onError func(http.ResponseWriter, *http.Request, int, string)) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ai, ok := AuthInfoFromContext(r.Context())
            if !ok {
                onError(w, r, http.StatusUnauthorized, "unauthorized")
                return
            }

            for _, s := range required {
                if _, ok := ai.Scopes[s]; !ok {
                    onError(w, r, http.StatusForbidden, "forbidden")
                    return
                }
            }

            next.ServeHTTP(w, r)
        })
    }
}
