package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"math/big"

	"github.com/google/uuid"
)

type KeySet struct {
	privateKey *rsa.PrivateKey
	kid        string
}

type JWKS struct {
	Keys []JWK `json:"keys"`
}

type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func NewKeySet() (*KeySet, error) {
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	return &KeySet{
		privateKey: pk,
		kid:        uuid.NewString(),
	}, nil
}

func (ks *KeySet) PrivateKey() *rsa.PrivateKey { return ks.privateKey }

func (ks *KeySet) PublicKey() *rsa.PublicKey {
	if ks.privateKey == nil {
		return nil
	}
	return &ks.privateKey.PublicKey
}

func (ks *KeySet) KeyID() string { return ks.kid }

func (ks *KeySet) JWKS() (JWKS, error) {
	pub := ks.PublicKey()
	if pub == nil {
		return JWKS{}, errors.New("missing public key")
	}

	return JWKS{
		Keys: []JWK{rsaPublicJWK(ks.kid, pub)},
	}, nil
}

func rsaPublicJWK(kid string, pub *rsa.PublicKey) JWK {
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())

	// RFC7517: exponent is base64url-encoded big-endian.
	eBytes := big.NewInt(int64(pub.E)).Bytes()
	e := base64.RawURLEncoding.EncodeToString(eBytes)

	return JWK{
		Kty: "RSA",
		Use: "sig",
		Alg: "RS256",
		Kid: kid,
		N:   n,
		E:   e,
	}
}
