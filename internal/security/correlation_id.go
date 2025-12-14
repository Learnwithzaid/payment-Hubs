package security

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

const CorrelationIDHeader = "X-Correlation-ID"

type correlationIDKey struct{}

func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cid := r.Header.Get(CorrelationIDHeader)
		if cid == "" {
			cid = uuid.NewString()
		}

		ctx := context.WithValue(r.Context(), correlationIDKey{}, cid)
		w.Header().Set(CorrelationIDHeader, cid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func CorrelationIDFromContext(ctx context.Context) string {
	if v := ctx.Value(correlationIDKey{}); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
