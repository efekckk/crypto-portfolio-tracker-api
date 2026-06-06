package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// ErrorResponse is the JSON envelope every 4xx/5xx response uses.
type ErrorResponse struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

// writeJSON serialises v with the given status code; status alone is sent if
// v is nil. Encode failures are logged but never panic.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: encode response: %v", err)
	}
}

// writeError emits a structured error envelope at the given status code.
func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, ErrorResponse{Error: code, Detail: detail})
}
