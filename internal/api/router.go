package api

import (
    "context"
    "log/slog"
    "net"
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"

    "github.com/example/pci-infra/internal/auth"
    "github.com/example/pci-infra/internal/ledger"
    "github.com/example/pci-infra/internal/security"
    "github.com/example/pci-infra/pkg/audit"
)

type Auditor interface {
    Append(payload string) *audit.LogEntry
}

type Dependencies struct {
    Logger       *slog.Logger
    OAuth        *auth.OAuthServer
    JWTValidator *auth.JWTValidator

    LedgerReader interface {
        ListAccounts(ctx context.Context, filter ledger.AccountFilter) ([]*ledger.Account, error)
        GetBalance(ctx context.Context, accountID string) (float64, error)
    }
    LedgerWriter interface {
        CreateAccount(ctx context.Context, req ledger.CreateAccountRequest) (*ledger.Account, error)
        Debit(ctx context.Context, req ledger.DebitRequest) error
        Credit(ctx context.Context, req ledger.CreditRequest) error
    }

    Auditor      Auditor
    RateLimiter  *security.RedisTokenBucket
    IPAllowlist  []*net.IPNet
    MaxBodyBytes int64
}

func NewRouter(deps Dependencies) (http.Handler, error) {
    if deps.Logger == nil {
        deps.Logger = slog.Default()
    }

    createAccountV, err := security.NewJSONSchemaValidator(createAccountSchema)
    if err != nil {
        return nil, err
    }
    debitV, err := security.NewJSONSchemaValidator(debitSchema)
    if err != nil {
        return nil, err
    }
    creditV, err := security.NewJSONSchemaValidator(creditSchema)
    if err != nil {
        return nil, err
    }

    onAuthError := func(w http.ResponseWriter, r *http.Request, status int, code string) {
        security.WriteJSONError(w, r, status, code)
    }

    r := chi.NewRouter()
    r.Use(middleware.Recoverer)
    r.Use(security.CorrelationID)
    r.Use(RequestLogger(deps.Logger))
    r.Use(security.BodySizeLimit(deps.MaxBodyBytes))
    r.Use(security.IPAllowlist(deps.IPAllowlist))
    if deps.RateLimiter != nil {
        r.Use(security.RateLimitMiddleware(deps.RateLimiter, rateLimitKeyByIP))
    }
    if deps.Auditor != nil {
        r.Use(AuditMiddleware(deps.Auditor))
    }

    r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    })

    if deps.OAuth != nil {
        r.Post("/oauth/token", deps.OAuth.TokenHandler)
        r.Get("/oauth/jwks.json", deps.OAuth.JWKSHandler)
    }

    r.Route("/v1", func(r chi.Router) {
        r.Use(auth.Authenticate(deps.JWTValidator, onAuthError))

        r.Route("/accounts", func(r chi.Router) {
            list := r.With(auth.RequireScopes("accounts:read", onAuthError))
            list.Get("/", handleListAccounts(deps))
            list.Get("", handleListAccounts(deps))

            create := r.With(auth.RequireScopes("accounts:write", onAuthError), createAccountV.Middleware)
            create.Post("/", handleCreateAccount(deps))
            create.Post("", handleCreateAccount(deps))
        })

        r.Route("/ledger", func(r chi.Router) {
            r.With(auth.RequireScopes("ledger:write", onAuthError), debitV.Middleware).Post("/debit", handleDebit(deps))
            r.With(auth.RequireScopes("ledger:write", onAuthError), creditV.Middleware).Post("/credit", handleCredit(deps))
            r.With(auth.RequireScopes("ledger:read", onAuthError)).Get("/balance", handleBalance(deps))
        })
    })

    r.NotFound(func(w http.ResponseWriter, r *http.Request) {
        security.WriteJSONError(w, r, http.StatusNotFound, "not_found")
    })

    r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
        security.WriteJSONError(w, r, http.StatusMethodNotAllowed, "method_not_allowed")
    })

    _ = debitV
    _ = creditV

    return r, nil
}

func rateLimitKeyByIP(r *http.Request) string {
    host, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        return ""
    }
    return "ip:" + host
}
