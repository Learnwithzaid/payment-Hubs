package api

import (
	"encoding/json"
	"net/http"

	"github.com/example/pci-infra/internal/security"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	cid := security.CorrelationIDFromContext(r.Context())
	if cid != "" {
		w.Header().Set(security.CorrelationIDHeader, cid)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
