package main

import (
    "context"
    "crypto/tls"
    "errors"
    "log/slog"
    "net"
    "net/http"
    "os"
    "os/signal"
    "strconv"
    "strings"
    "syscall"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/redis/go-redis/v9"

    "github.com/example/pci-infra/internal/api"
    "github.com/example/pci-infra/internal/auth"
    "github.com/example/pci-infra/internal/ledger"
    "github.com/example/pci-infra/internal/security"
    "github.com/example/pci-infra/pkg/audit"
)

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
    slog.SetDefault(logger)

    addr := getenv("API_ADDR", ":8443")
    certFile := os.Getenv("API_TLS_CERT")
    keyFile := os.Getenv("API_TLS_KEY")
    caFile := os.Getenv("API_TLS_CA")
    if certFile == "" || keyFile == "" || caFile == "" {
        logger.Error("missing TLS env vars", "API_TLS_CERT", certFile != "", "API_TLS_KEY", keyFile != "", "API_TLS_CA", caFile != "")
        os.Exit(1)
    }

    maxBody := int64(getenvInt("API_MAX_BODY_BYTES", 1<<20))

    allowCIDRs := strings.Split(getenv("API_IP_ALLOWLIST", ""), ",")
    allowlist, err := security.ParseCIDRAllowlist(allowCIDRs)
    if err != nil {
        logger.Error("invalid API_IP_ALLOWLIST", "error", err)
        os.Exit(1)
    }

    pool, err := pgxpool.New(context.Background(), getenv("DATABASE_URL", ""))
    if err != nil {
        logger.Error("failed to create postgres pool", "error", err)
        os.Exit(1)
    }
    defer pool.Close()

    redisClient := redis.NewClient(&redis.Options{Addr: getenv("REDIS_ADDR", "localhost:6379")})
    defer redisClient.Close()

    keySet, err := auth.NewKeySet()
    if err != nil {
        logger.Error("failed to create keyset", "error", err)
        os.Exit(1)
    }

    oauthServer := &auth.OAuthServer{
        Store:          &auth.PostgresClientStore{Pool: pool},
        Keys:           keySet,
        Issuer:         "ledger-api",
        AccessTokenTTL: 15 * time.Minute,
    }

    jwtValidator := &auth.JWTValidator{KeySet: keySet, Issuer: "ledger-api"}

    pl := ledger.NewPostgresLedger(pool)
    ls := ledger.NewLedgerService(pl)

    auditor := audit.NewChainLogger()

    rateLimiter := &security.RedisTokenBucket{
        Redis:      redisClient,
        Prefix:     "ledger_api",
        Capacity:   getenvInt("API_RATE_LIMIT_CAPACITY", 20),
        RefillRate: float64(getenvInt("API_RATE_LIMIT_REFILL_PER_SEC", 10)),
    }

    router, err := api.NewRouter(api.Dependencies{
        Logger:       logger,
        OAuth:        oauthServer,
        JWTValidator: jwtValidator,
        LedgerReader: ls,
        LedgerWriter: ls,
        Auditor:      auditor,
        RateLimiter:  rateLimiter,
        IPAllowlist:  allowlist,
        MaxBodyBytes: maxBody,
    })
    if err != nil {
        logger.Error("failed to build router", "error", err)
        os.Exit(1)
    }

    tlsCfg, err := security.LoadServerTLSConfig(security.TLSConfig{
        CertFile:          certFile,
        KeyFile:           keyFile,
        CAFile:            caFile,
        RequireClientAuth: true,
    })
    if err != nil {
        logger.Error("failed to load TLS config", "error", err)
        os.Exit(1)
    }

    srv := &http.Server{
        Addr:              addr,
        Handler:           router,
        ReadHeaderTimeout: 5 * time.Second,
        TLSConfig:         tlsCfg,
    }

    ln, err := net.Listen("tcp", addr)
    if err != nil {
        logger.Error("failed to listen", "error", err)
        os.Exit(1)
    }

    tlsListener := tls.NewListener(ln, tlsCfg)

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

    go func() {
        <-sigCh
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        _ = srv.Shutdown(ctx)
    }()

    logger.Info("ledger api gateway listening", "addr", addr)
    if err := srv.Serve(tlsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
        logger.Error("server error", "error", err)
        os.Exit(1)
    }
}

func getenv(key, def string) string {
    v := os.Getenv(key)
    if v == "" {
        return def
    }
    return v
}

func getenvInt(key string, def int) int {
    v := os.Getenv(key)
    if v == "" {
        return def
    }
    i, err := strconv.Atoi(v)
    if err != nil {
        return def
    }
    return i
}

