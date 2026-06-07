package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"nlserver/internal/repository"
)

const aiStreamReceiptCleanupTimeout = 5 * time.Second

type aiStreamReceiptClaim struct {
	Key           string
	OperationType string
	Claimed       bool
	Receipt       repository.OperationReceipt
}

type aiStreamReceiptBody struct {
	Event     string                     `json:"event"`
	Done      *aiStreamDoneResponse      `json:"done,omitempty"`
	Cancelled *aiStreamCancelledResponse `json:"cancelled,omitempty"`
}

func aiStreamOperationType(conversationID string) string {
	return "ai.message.stream:" + conversationID
}

func (s *Server) claimAIStreamReceipt(ctx context.Context, userID string, operationType string, key string) (aiStreamReceiptClaim, *requestError, error) {
	key = strings.TrimSpace(key)
	if err := validateIdempotencyKey(key); err != nil {
		return aiStreamReceiptClaim{}, err, nil
	}
	if key == "" {
		return aiStreamReceiptClaim{OperationType: operationType}, nil, nil
	}
	if s.repos.OperationReceipts == nil {
		return aiStreamReceiptClaim{}, nil, errors.New("operation receipt repository is not configured")
	}

	pendingPayload, err := marshalReceiptPayload(idempotentReceiptPayload{Status: idempotencyPendingStatus})
	if err != nil {
		return aiStreamReceiptClaim{}, nil, err
	}
	receipt, claimed, err := s.repos.OperationReceipts.Claim(ctx, userID, key, operationType, pendingPayload)
	if err != nil {
		return aiStreamReceiptClaim{}, nil, err
	}
	return aiStreamReceiptClaim{
		Key:           key,
		OperationType: operationType,
		Claimed:       claimed,
		Receipt:       receipt,
	}, nil, nil
}

func (s *Server) completeAIStreamReceipt(ctx context.Context, userID string, claim aiStreamReceiptClaim, done aiStreamDoneResponse) error {
	return s.completeAIStreamFinalReceipt(ctx, userID, claim, aiStreamReceiptBody{
		Event: "ai.stream.done",
		Done:  &done,
	})
}

func (s *Server) completeAIStreamCancelledReceipt(ctx context.Context, userID string, claim aiStreamReceiptClaim, cancelled aiStreamCancelledResponse) error {
	return s.completeAIStreamFinalReceipt(ctx, userID, claim, aiStreamReceiptBody{
		Event:     "ai.stream.cancelled",
		Cancelled: &cancelled,
	})
}

func (s *Server) completeAIStreamFinalReceipt(ctx context.Context, userID string, claim aiStreamReceiptClaim, final aiStreamReceiptBody) error {
	if claim.Key == "" || !claim.Claimed || s.repos.OperationReceipts == nil {
		return nil
	}
	body, err := json.Marshal(final)
	if err != nil {
		return err
	}
	completedPayload, err := marshalReceiptPayload(idempotentReceiptPayload{
		Status: idempotencyCompletedStatus,
		Body:   body,
	})
	if err != nil {
		return err
	}
	_, err = s.repos.OperationReceipts.Complete(ctx, userID, claim.Key, claim.OperationType, completedPayload)
	return err
}

func (s *Server) deleteAIStreamReceipt(userID string, claim aiStreamReceiptClaim) {
	if claim.Key == "" || !claim.Claimed || s.repos.OperationReceipts == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), aiStreamReceiptCleanupTimeout)
	defer cancel()
	if err := s.repos.OperationReceipts.Delete(ctx, userID, claim.Key, claim.OperationType); err != nil {
		s.logger.WarnContext(ctx, "failed to delete pending AI stream receipt", "error", err, "operation_type", claim.OperationType)
	}
}

func aiStreamFinalFromReceipt(receipt repository.OperationReceipt, streamID string) (string, aiStreamDoneResponse, aiStreamCancelledResponse, string, string) {
	payload, err := unmarshalReceiptPayload(receipt.ServerResult)
	if err != nil {
		return "", aiStreamDoneResponse{}, aiStreamCancelledResponse{}, "IDEMPOTENCY_KEY_REUSED", "Idempotency key was already used for a different operation result"
	}
	switch payload.Status {
	case idempotencyPendingStatus:
		return "", aiStreamDoneResponse{}, aiStreamCancelledResponse{}, "OPERATION_IN_PROGRESS", "Operation is still in progress"
	case idempotencyCompletedStatus:
		if len(payload.Body) == 0 {
			return "", aiStreamDoneResponse{}, aiStreamCancelledResponse{}, "IDEMPOTENCY_KEY_REUSED", "Idempotency key was already used for a different operation result"
		}
		var final aiStreamReceiptBody
		if err := json.Unmarshal(payload.Body, &final); err == nil && final.Event != "" {
			switch final.Event {
			case "ai.stream.done":
				if final.Done == nil {
					return "", aiStreamDoneResponse{}, aiStreamCancelledResponse{}, "IDEMPOTENCY_KEY_REUSED", "Idempotency key was already used for a different operation result"
				}
				final.Done.StreamID = streamID
				return final.Event, *final.Done, aiStreamCancelledResponse{}, "", ""
			case "ai.stream.cancelled":
				if final.Cancelled == nil {
					return "", aiStreamDoneResponse{}, aiStreamCancelledResponse{}, "IDEMPOTENCY_KEY_REUSED", "Idempotency key was already used for a different operation result"
				}
				final.Cancelled.StreamID = streamID
				return final.Event, aiStreamDoneResponse{}, *final.Cancelled, "", ""
			default:
				return "", aiStreamDoneResponse{}, aiStreamCancelledResponse{}, "IDEMPOTENCY_KEY_REUSED", "Idempotency key was already used for a different operation result"
			}
		}

		var done aiStreamDoneResponse
		if err := json.Unmarshal(payload.Body, &done); err != nil {
			return "", aiStreamDoneResponse{}, aiStreamCancelledResponse{}, "IDEMPOTENCY_KEY_REUSED", "Idempotency key was already used for a different operation result"
		}
		done.StreamID = streamID
		return "ai.stream.done", done, aiStreamCancelledResponse{}, "", ""
	default:
		return "", aiStreamDoneResponse{}, aiStreamCancelledResponse{}, "IDEMPOTENCY_KEY_REUSED", "Idempotency key was already used for a different operation result"
	}
}

func writeAIStreamServiceNotReady(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Operation receipt repository is not configured")
}
