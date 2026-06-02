package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"nlserver/internal/repository"
)

type operationReceiptRequest struct {
	ClientOperationID string          `json:"clientOperationId"`
	OperationType     string          `json:"operationType"`
	Payload           json.RawMessage `json:"payload"`
}

type operationReceiptResponse struct {
	Status       string          `json:"status"`
	ServerResult json.RawMessage `json:"serverResult"`
}

func (s *Server) handleOperationReceipt(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.repos.OperationReceipts == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Operation receipt repository is not configured")
		return
	}

	var req operationReceiptRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if strings.TrimSpace(req.ClientOperationID) == "" || strings.TrimSpace(req.OperationType) == "" {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "clientOperationId and operationType are required")
		return
	}
	payload := req.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	receipt, err := s.repos.OperationReceipts.GetOrCreate(r.Context(), repository.CreateOperationReceiptParams{
		UserID:            principal.UserID,
		ClientOperationID: req.ClientOperationID,
		OperationType:     req.OperationType,
		ServerResult:      payload,
	})
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, operationReceiptResponse{
		Status:       "ok",
		ServerResult: receipt.ServerResult,
	})
}
