package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// writeJSON encodes v as the response body with the given status code.
func writeJSON(w http.ResponseWriter, logger *slog.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status line is already sent, so an encode error here can only be logged,
	// not turned into a different response.
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error("encoding response", "error", err)
	}
}

// apiError is the JSON body writeError sends.
type apiError struct {
	Error string `json:"error"`
}

// writeError writes a JSON error body.
func writeError(w http.ResponseWriter, logger *slog.Logger, status int, message string) {
	writeJSON(w, logger, status, apiError{Error: message})
}

// decodeJSON decodes the request body into v.
func decodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("decoding request body: %w", err)
	}
	return nil
}
