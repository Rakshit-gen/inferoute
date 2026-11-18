// Package apierr writes errors in the JSON shape OpenAI clients expect.
package apierr

import (
	"encoding/json"
	"net/http"
)

// Error types used across the gateway.
const (
	InvalidRequest = "invalid_request_error"
	Authentication = "authentication_error"
	RateLimit      = "rate_limit_error"
	Server         = "server_error"
)

// Write sends status with an OpenAI-compatible error body.
func Write(w http.ResponseWriter, status int, errType, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Del("Content-Length")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
		"message": message,
		"type":    errType,
		"code":    code,
	}})
}
