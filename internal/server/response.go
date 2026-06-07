package server

import (
	"encoding/json"
	"net/http"
)

type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
	Details   any    `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	writeErrorDetails(w, r, status, code, message, nil)
}

func writeErrorCause(w http.ResponseWriter, r *http.Request, status int, code string, message string, cause error) {
	writeErrorDetailsCause(w, r, status, code, message, nil, cause)
}

func writeErrorDetails(w http.ResponseWriter, r *http.Request, status int, code string, message string, details any) {
	writeErrorDetailsCause(w, r, status, code, message, details, nil)
}

func writeErrorDetailsCause(w http.ResponseWriter, r *http.Request, status int, code string, message string, details any, cause error) {
	setAccessLogError(r.Context(), code, message, cause)
	writeJSON(w, status, errorResponse{
		Code:      code,
		Message:   message,
		RequestID: requestIDFromContext(r.Context()),
		Details:   details,
	})
}
