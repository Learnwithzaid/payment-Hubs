package security

import (
	"encoding/json"
	"net/http"
)

type ErrorResponse struct {
	Error         string `json:"error"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

func WriteJSONError(w http.ResponseWriter, r *http.Request, status int, code string) {
	cid := CorrelationIDFromContext(r.Context())
	if cid != "" {
		w.Header().Set(CorrelationIDHeader, cid)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Error:         code,
		CorrelationID: cid,
	})
}
