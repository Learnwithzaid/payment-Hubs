package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/example/pci-infra/internal/security"
)

func RequestLogger(l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if l == nil {
				next.ServeHTTP(w, r)
				return
			}

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(sw, r)
			dur := time.Since(start)

			l.Info("http_request",
				"cid", security.CorrelationIDFromContext(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", dur.Milliseconds(),
			)
		})
	}
}
