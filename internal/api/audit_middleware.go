package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/example/pci-infra/internal/security"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func AuditMiddleware(a Auditor) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			start := time.Now()
			next.ServeHTTP(sw, r)
			dur := time.Since(start)

			cid := security.CorrelationIDFromContext(r.Context())
			payload := fmt.Sprintf("cid=%s method=%s path=%s status=%d dur_ms=%d", cid, r.Method, r.URL.Path, sw.status, dur.Milliseconds())
			a.Append(payload)
		})
	}
}
