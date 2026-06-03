package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode"

	"nlserver/internal/repository"
)

const (
	idempotencyPendingStatus   = "pending"
	idempotencyCompletedStatus = "completed"
	maxIdempotencyKeyLength    = 128
)

type idempotentHTTPResult struct {
	Status int
	ETag   string
	Body   any
	After  func()
}

type idempotentReceiptPayload struct {
	Status     string          `json:"status"`
	HTTPStatus int             `json:"httpStatus,omitempty"`
	ETag       string          `json:"etag,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

func idempotencyKeyFromRequest(r *http.Request, clientOperationID string) string {
	if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
		return key
	}
	return strings.TrimSpace(clientOperationID)
}

func validateIdempotencyKey(key string) *requestError {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	if len(key) > maxIdempotencyKeyLength {
		return &requestError{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR", Message: "idempotency key is too long"}
	}
	for _, r := range key {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '-', '_', '.', ':':
			continue
		default:
			return &requestError{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR", Message: "idempotency key contains invalid characters"}
		}
	}
	return nil
}

func (s *Server) handleIdempotentWrite(
	w http.ResponseWriter,
	r *http.Request,
	userID string,
	operationType string,
	idempotencyKey string,
	writeOperationError func(error),
	operation func() (idempotentHTTPResult, error),
) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if idempotencyKey == "" {
		result, err := operation()
		if err != nil {
			writeOperationError(err)
			return
		}
		writeIdempotentHTTPResult(w, r, result)
		return
	}
	if s.repos.OperationReceipts == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Operation receipt repository is not configured")
		return
	}

	pendingPayload, err := marshalReceiptPayload(idempotentReceiptPayload{Status: idempotencyPendingStatus})
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return
	}
	receipt, claimed, err := s.repos.OperationReceipts.Claim(r.Context(), userID, idempotencyKey, operationType, pendingPayload)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	if !claimed {
		s.writeExistingIdempotentReceipt(w, r, receipt)
		return
	}

	result, err := operation()
	if err != nil {
		if deleteErr := s.repos.OperationReceipts.Delete(r.Context(), userID, idempotencyKey, operationType); deleteErr != nil {
			s.logger.WarnContext(r.Context(), "failed to delete pending operation receipt", "error", deleteErr, "operation_type", operationType)
		}
		writeOperationError(err)
		return
	}
	body, err := json.Marshal(result.Body)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return
	}
	completedPayload, err := marshalReceiptPayload(idempotentReceiptPayload{
		Status:     idempotencyCompletedStatus,
		HTTPStatus: result.Status,
		ETag:       result.ETag,
		Body:       body,
	})
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return
	}
	if _, err := s.repos.OperationReceipts.Complete(r.Context(), userID, idempotencyKey, operationType, completedPayload); err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeIdempotentHTTPResult(w, r, result)
}

func (s *Server) writeExistingIdempotentReceipt(w http.ResponseWriter, r *http.Request, receipt repository.OperationReceipt) {
	payload, err := unmarshalReceiptPayload(receipt.ServerResult)
	if err != nil {
		writeError(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "Idempotency key was already used for a different operation result")
		return
	}
	switch payload.Status {
	case idempotencyPendingStatus:
		writeError(w, r, http.StatusConflict, "OPERATION_IN_PROGRESS", "Operation is still in progress")
	case idempotencyCompletedStatus:
		status := payload.HTTPStatus
		if status == 0 {
			status = http.StatusOK
		}
		if payload.ETag != "" {
			w.Header().Set("ETag", payload.ETag)
		}
		writeJSON(w, status, payloadBodyOrEmpty(payload.Body))
	default:
		writeError(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "Idempotency key was already used for a different operation result")
	}
}

func writeIdempotentHTTPResult(w http.ResponseWriter, r *http.Request, result idempotentHTTPResult) {
	if result.Status == 0 {
		result.Status = http.StatusOK
	}
	if result.After != nil {
		result.After()
	}
	if result.ETag != "" {
		writeJSONWithETag(w, r, result.Status, result.ETag, result.Body)
		return
	}
	writeJSON(w, result.Status, result.Body)
}

func marshalReceiptPayload(payload idempotentReceiptPayload) (json.RawMessage, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func unmarshalReceiptPayload(raw json.RawMessage) (idempotentReceiptPayload, error) {
	var payload idempotentReceiptPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return idempotentReceiptPayload{}, err
	}
	if payload.Status == "" {
		return idempotentReceiptPayload{}, errors.New("missing receipt status")
	}
	return payload, nil
}

func payloadBodyOrEmpty(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	return raw
}
