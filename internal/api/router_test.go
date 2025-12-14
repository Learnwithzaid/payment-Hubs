package api

import (
    "bytes"
    "context"
    "crypto/rand"
    "crypto/rsa"
    "crypto/tls"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/json"
    "encoding/pem"
    "math/big"
    "net"
    "net/http"
    "net/http/httptest"
    "net/url"
    "testing"
    "time"

    "github.com/alicebob/miniredis/v2"
    "github.com/redis/go-redis/v9"
    "github.com/stretchr/testify/require"

    "github.com/example/pci-infra/internal/auth"
    "github.com/example/pci-infra/internal/ledger"
    "github.com/example/pci-infra/internal/security"
    "github.com/example/pci-infra/pkg/audit"
)

type memoryClientStore struct {
    clients map[string]*auth.Client
}

func (m *memoryClientStore) GetClient(ctx context.Context, clientID string) (*auth.Client, error) {
    c := m.clients[clientID]
    if c == nil {
        return nil, auth.ErrClientNotFound
    }
    return c, nil
}

type fakeLedger struct {
    createCalls int
    debitCalls  int
    creditCalls int
}

func (f *fakeLedger) ListAccounts(ctx context.Context, filter ledger.AccountFilter) ([]*ledger.Account, error) {
    return []*ledger.Account{{ID: "acc-1", AccountNumber: "A1", AccountType: "asset", Name: "Test", CurrencyCode: "USD"}}, nil
}

func (f *fakeLedger) GetBalance(ctx context.Context, accountID string) (float64, error) {
    return 123.45, nil
}

func (f *fakeLedger) CreateAccount(ctx context.Context, req ledger.CreateAccountRequest) (*ledger.Account, error) {
    f.createCalls++
    return &ledger.Account{ID: "acc-1", AccountNumber: req.AccountNumber, AccountType: req.AccountType, Name: req.Name, CurrencyCode: req.CurrencyCode, CreatedBy: req.CreatedBy}, nil
}

func (f *fakeLedger) Debit(ctx context.Context, req ledger.DebitRequest) error {
    f.debitCalls++
    return nil
}

func (f *fakeLedger) Credit(ctx context.Context, req ledger.CreditRequest) error {
    f.creditCalls++
    return nil
}

type auditSpy struct{ calls int }

func (a *auditSpy) Append(payload string) *audit.LogEntry {
    a.calls++
    return &audit.LogEntry{Hash: payload}
}

func TestMTLSRequired(t *testing.T) {
    deps, tlsCfg, clientTLS, noClientTLS := newTestDeps(t)

    h, err := NewRouter(deps)
    require.NoError(t, err)

    ts := httptest.NewUnstartedServer(h)
    ts.TLS = tlsCfg
    ts.StartTLS()
    defer ts.Close()

    clientNoCert := &http.Client{Transport: &http.Transport{TLSClientConfig: noClientTLS}}
    _, err = clientNoCert.Get(ts.URL + "/healthz")
    require.Error(t, err)

    clientWithCert := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS}}
    resp, err := clientWithCert.Get(ts.URL + "/healthz")
    require.NoError(t, err)
    require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAuthFailuresAndValidation(t *testing.T) {
    deps, tlsCfg, clientTLS, _ := newTestDeps(t)

    h, err := NewRouter(deps)
    require.NoError(t, err)

    ts := httptest.NewUnstartedServer(h)
    ts.TLS = tlsCfg
    ts.StartTLS()
    defer ts.Close()

    client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS}}

    resp, err := client.Get(ts.URL + "/v1/accounts")
    require.NoError(t, err)
    require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

    token := issueToken(t, deps, "read-client", "read-secret", "accounts:read")

    reqBody := map[string]any{
        "account_type":  "asset",
        "name":          "Test",
        "currency_code": "USD",
        "created_by":    "tester",
    }
    b, _ := json.Marshal(reqBody)

    req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/accounts", bytes.NewReader(b))
    req.Header.Set("Authorization", "Bearer "+token)
    resp, err = client.Do(req)
    require.NoError(t, err)
    require.Equal(t, http.StatusForbidden, resp.StatusCode)

    // validation error before ledger call
    token = issueToken(t, deps, "write-client", "write-secret", "accounts:write")
    req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/accounts", bytes.NewReader(b))
    req.Header.Set("Authorization", "Bearer "+token)
    resp, err = client.Do(req)
    require.NoError(t, err)
    require.Equal(t, http.StatusBadRequest, resp.StatusCode)
    fl := deps.LedgerWriter.(*fakeLedger)
    require.Equal(t, 0, fl.createCalls)
}

func TestRateLimitTrips(t *testing.T) {
    deps, tlsCfg, clientTLS, _ := newTestDeps(t)
    deps.RateLimiter.Capacity = 1
    deps.RateLimiter.RefillRate = 0.0000001

    h, err := NewRouter(deps)
    require.NoError(t, err)

    ts := httptest.NewUnstartedServer(h)
    ts.TLS = tlsCfg
    ts.StartTLS()
    defer ts.Close()

    client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS}}

    resp, err := client.Get(ts.URL + "/oauth/jwks.json")
    require.NoError(t, err)
    require.Equal(t, http.StatusOK, resp.StatusCode)

    resp, err = client.Get(ts.URL + "/oauth/jwks.json")
    require.NoError(t, err)
    require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
}

func TestBodySizeLimit(t *testing.T) {
    deps, tlsCfg, clientTLS, _ := newTestDeps(t)
    deps.MaxBodyBytes = 32

    h, err := NewRouter(deps)
    require.NoError(t, err)

    ts := httptest.NewUnstartedServer(h)
    ts.TLS = tlsCfg
    ts.StartTLS()
    defer ts.Close()

    client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS}}
    token := issueToken(t, deps, "write-client", "write-secret", "accounts:write")

    // body > 32 bytes
    bigBody := []byte(`{"account_number":"ACC001","account_type":"asset","name":"Main","currency_code":"USD","created_by":"tester"}`)
    req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/accounts", bytes.NewReader(bigBody))
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")
    resp, err := client.Do(req)
    require.NoError(t, err)
    require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

func TestSuccessfulFlow(t *testing.T) {
    deps, tlsCfg, clientTLS, _ := newTestDeps(t)

    h, err := NewRouter(deps)
    require.NoError(t, err)

    ts := httptest.NewUnstartedServer(h)
    ts.TLS = tlsCfg
    ts.StartTLS()
    defer ts.Close()

    client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS}}

    token := issueToken(t, deps, "full-client", "full-secret", "accounts:write accounts:read ledger:read ledger:write")

    createReq := map[string]any{
        "account_number": "ACC001",
        "account_type":   "asset",
        "name":           "Main",
        "currency_code":  "USD",
        "created_by":     "tester",
    }
    b, _ := json.Marshal(createReq)
    req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/accounts", bytes.NewReader(b))
    req.Header.Set("Authorization", "Bearer "+token)
    resp, err := client.Do(req)
    require.NoError(t, err)
    require.Equal(t, http.StatusCreated, resp.StatusCode)
    require.NotEmpty(t, resp.Header.Get(security.CorrelationIDHeader))

    // debit
    debitReq := map[string]any{"account_id": "acc-1", "amount": 10, "currency_code": "USD", "created_by": "tester"}
    b, _ = json.Marshal(debitReq)
    req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/ledger/debit", bytes.NewReader(b))
    req.Header.Set("Authorization", "Bearer "+token)
    resp, err = client.Do(req)
    require.NoError(t, err)
    require.Equal(t, http.StatusOK, resp.StatusCode)

    // balance
    req, _ = http.NewRequest(http.MethodGet, ts.URL+"/v1/ledger/balance?account_id=acc-1", nil)
    req.Header.Set("Authorization", "Bearer "+token)
    resp, err = client.Do(req)
    require.NoError(t, err)
    require.Equal(t, http.StatusOK, resp.StatusCode)
}

func issueToken(t *testing.T, deps Dependencies, clientID, clientSecret, scope string) string {
    ts := httptest.NewServer(http.HandlerFunc(deps.OAuth.TokenHandler))
    defer ts.Close()

    form := []byte("grant_type=client_credentials&scope=" + url.QueryEscape(scope))
    req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(form))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    req.SetBasicAuth(clientID, clientSecret)

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()
    require.Equal(t, http.StatusOK, resp.StatusCode)

    var tr struct {
        AccessToken string `json:"access_token"`
    }
    require.NoError(t, json.NewDecoder(resp.Body).Decode(&tr))
    require.NotEmpty(t, tr.AccessToken)
    return tr.AccessToken
}

type testCerts struct {
    serverTLS   *tls.Config
    clientTLS   *tls.Config
    noClientTLS *tls.Config
}

func newTestDeps(t *testing.T) (Dependencies, *tls.Config, *tls.Config, *tls.Config) {
    certs := generateMTLSCerts(t)

    mr := miniredis.RunT(t)
    rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

    keySet, err := auth.NewKeySet()
    require.NoError(t, err)

    store := &memoryClientStore{clients: map[string]*auth.Client{}}
    store.clients["read-client"] = &auth.Client{ID: "read-client", SecretHash: mustHash(t, "read-secret"), Scopes: []string{"accounts:read"}}
    store.clients["write-client"] = &auth.Client{ID: "write-client", SecretHash: mustHash(t, "write-secret"), Scopes: []string{"accounts:write"}}
    store.clients["full-client"] = &auth.Client{ID: "full-client", SecretHash: mustHash(t, "full-secret"), Scopes: []string{"accounts:read", "accounts:write", "ledger:read", "ledger:write"}}

    oauthServer := &auth.OAuthServer{Store: store, Keys: keySet, Issuer: "test", AccessTokenTTL: 5 * time.Minute}
    validator := &auth.JWTValidator{KeySet: keySet, Issuer: "test"}

    fl := &fakeLedger{}
    as := &auditSpy{}

    deps := Dependencies{
        OAuth:        oauthServer,
        JWTValidator: validator,
        LedgerReader: fl,
        LedgerWriter: fl,
        Auditor:      as,
        RateLimiter:  &security.RedisTokenBucket{Redis: rdb, Prefix: "test", Capacity: 100, RefillRate: 100},
        IPAllowlist:  nil,
        MaxBodyBytes: 1 << 20,
    }

    return deps, certs.serverTLS, certs.clientTLS, certs.noClientTLS
}

func mustHash(t *testing.T, secret string) string {
    h, err := auth.HashClientSecret(secret)
    require.NoError(t, err)
    return h
}

func generateMTLSCerts(t *testing.T) *testCerts {
    caKey, err := rsa.GenerateKey(rand.Reader, 2048)
    require.NoError(t, err)

    caTmpl := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: "test-ca"},
        NotBefore:    time.Now().Add(-time.Hour),
        NotAfter:     time.Now().Add(time.Hour),
        IsCA:         true,
        KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
        BasicConstraintsValid: true,
    }
    caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
    require.NoError(t, err)
    caCert, err := x509.ParseCertificate(caDER)
    require.NoError(t, err)

    caPool := x509.NewCertPool()
    caPool.AddCert(caCert)

    serverCert := signCert(t, caCert, caKey, "server", []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, nil, []net.IP{net.ParseIP("127.0.0.1")})
    clientCert := signCert(t, caCert, caKey, "client", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, nil, nil)

    serverTLS := &tls.Config{
        Certificates: []tls.Certificate{serverCert},
        ClientAuth:   tls.RequireAndVerifyClientCert,
        ClientCAs:    caPool,
        MinVersion:   tls.VersionTLS13,
    }
    clientTLS := &tls.Config{
        Certificates: []tls.Certificate{clientCert},
        RootCAs:      caPool,
        MinVersion:   tls.VersionTLS13,
    }
    noClientTLS := &tls.Config{
        RootCAs:    caPool,
        MinVersion: tls.VersionTLS13,
    }

    return &testCerts{serverTLS: serverTLS, clientTLS: clientTLS, noClientTLS: noClientTLS}
}

func signCert(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, cn string, eku []x509.ExtKeyUsage, dns []string, ips []net.IP) tls.Certificate {
    key, err := rsa.GenerateKey(rand.Reader, 2048)
    require.NoError(t, err)

    tmpl := &x509.Certificate{
        SerialNumber: big.NewInt(time.Now().UnixNano()),
        Subject:      pkix.Name{CommonName: cn},
        NotBefore:    time.Now().Add(-time.Hour),
        NotAfter:     time.Now().Add(time.Hour),
        KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
        ExtKeyUsage:  eku,
        DNSNames:     dns,
        IPAddresses:  ips,
    }

    der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
    require.NoError(t, err)

    certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
    keyDER, err := x509.MarshalPKCS8PrivateKey(key)
    require.NoError(t, err)
    keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

    c, err := tls.X509KeyPair(certPEM, keyPEM)
    require.NoError(t, err)
    return c
}
