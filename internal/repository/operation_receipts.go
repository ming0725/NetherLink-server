package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type OperationReceiptRepository struct {
	db DBTX
}

type OperationReceipt struct {
	ID                string
	UserID            string
	ClientOperationID string
	OperationType     string
	ServerResult      json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type CreateOperationReceiptParams struct {
	UserID            string
	ClientOperationID string
	OperationType     string
	ServerResult      json.RawMessage
}

func NewOperationReceiptRepository(db DBTX) *OperationReceiptRepository {
	return &OperationReceiptRepository{db: db}
}

func (r *OperationReceiptRepository) Get(ctx context.Context, userID string, clientOperationID string, operationType string) (OperationReceipt, error) {
	receipt, err := scanOperationReceipt(r.db.QueryRow(ctx, `
		SELECT
			id,
			user_id,
			client_operation_id,
			operation_type,
			server_result_json,
			created_at,
			updated_at
		FROM operation_receipts
		WHERE user_id = $1::uuid
		  AND client_operation_id = $2
		  AND operation_type = $3`,
		normalizeText(userID),
		normalizeText(clientOperationID),
		normalizeText(operationType),
	))
	return receipt, translateReadError(err)
}

func (r *OperationReceiptRepository) Claim(ctx context.Context, userID string, clientOperationID string, operationType string, pendingResult json.RawMessage) (OperationReceipt, bool, error) {
	receipt, err := scanOperationReceipt(r.db.QueryRow(ctx, `
		INSERT INTO operation_receipts (
			user_id,
			client_operation_id,
			operation_type,
			server_result_json
		)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (user_id, client_operation_id, operation_type) DO NOTHING
		RETURNING
			id,
			user_id,
			client_operation_id,
			operation_type,
			server_result_json,
			created_at,
			updated_at`,
		normalizeText(userID),
		normalizeText(clientOperationID),
		normalizeText(operationType),
		jsonOrDefault(pendingResult),
	))
	if err == nil {
		return receipt, true, nil
	}
	if translated := translateWriteError(err); translated != ErrNotFound {
		return OperationReceipt{}, false, translated
	}

	existing, err := r.Get(ctx, userID, clientOperationID, operationType)
	return existing, false, err
}

func (r *OperationReceiptRepository) Complete(ctx context.Context, userID string, clientOperationID string, operationType string, serverResult json.RawMessage) (OperationReceipt, error) {
	receipt, err := scanOperationReceipt(r.db.QueryRow(ctx, `
		UPDATE operation_receipts
		SET server_result_json = $4,
		    updated_at = now()
		WHERE user_id = $1::uuid
		  AND client_operation_id = $2
		  AND operation_type = $3
		RETURNING
			id,
			user_id,
			client_operation_id,
			operation_type,
			server_result_json,
			created_at,
			updated_at`,
		normalizeText(userID),
		normalizeText(clientOperationID),
		normalizeText(operationType),
		jsonOrDefault(serverResult),
	))
	return receipt, translateWriteError(err)
}

func (r *OperationReceiptRepository) Delete(ctx context.Context, userID string, clientOperationID string, operationType string) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM operation_receipts
		WHERE user_id = $1::uuid
		  AND client_operation_id = $2
		  AND operation_type = $3`,
		normalizeText(userID),
		normalizeText(clientOperationID),
		normalizeText(operationType),
	)
	return translateWriteError(err)
}

func (r *OperationReceiptRepository) GetOrCreate(ctx context.Context, params CreateOperationReceiptParams) (OperationReceipt, error) {
	receipt, err := scanOperationReceipt(r.db.QueryRow(ctx, `
		INSERT INTO operation_receipts (
			user_id,
			client_operation_id,
			operation_type,
			server_result_json
		)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (user_id, client_operation_id, operation_type) DO NOTHING
		RETURNING
			id,
			user_id,
			client_operation_id,
			operation_type,
			server_result_json,
			created_at,
			updated_at`,
		normalizeText(params.UserID),
		normalizeText(params.ClientOperationID),
		normalizeText(params.OperationType),
		jsonOrDefault(params.ServerResult),
	))
	if err == nil {
		return receipt, nil
	}
	if translated := translateWriteError(err); translated != ErrNotFound {
		return OperationReceipt{}, translated
	}

	return r.Get(ctx, params.UserID, params.ClientOperationID, params.OperationType)
}

func scanOperationReceipt(row scanner) (OperationReceipt, error) {
	var (
		receipt OperationReceipt
		id      pgtype.UUID
		userID  pgtype.UUID
	)

	err := row.Scan(
		&id,
		&userID,
		&receipt.ClientOperationID,
		&receipt.OperationType,
		&receipt.ServerResult,
		&receipt.CreatedAt,
		&receipt.UpdatedAt,
	)
	if err != nil {
		return OperationReceipt{}, err
	}

	receipt.ID = uuidString(id)
	receipt.UserID = uuidString(userID)
	receipt.ServerResult = jsonOrDefault(receipt.ServerResult)
	return receipt, nil
}
