package server

import (
	"encoding/json"
	"net/http"
)

type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	writeJSON(w, status, errorResponse{
		Code:      code,
		Message:   message,
		RequestID: requestIDFromContext(r.Context()),
	})
}
