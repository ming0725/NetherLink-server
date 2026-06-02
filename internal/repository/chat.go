package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const messageSelectColumns = `
	m.id,
	m.conversation_id,
	m.sender_id,
	m.message_seq,
	m.type,
	m.content_text,
	m.content_json,
	m.referenced_message_id,
	m.client_message_id,
	m.client_sent_at,
	m.server_received_at,
	m.created_at,
	m.updated_at,
	m.edited_at,
	m.recalled_at`

type ChatRepository struct {
	db DBTX
}

type Message struct {
	ID                  string
	ConversationID      string
	SenderID            string
	MessageSeq          int64
	Type                string
	ContentText         *string
	ContentJSON         json.RawMessage
	ReferencedMessageID *string
	ClientMessageID     string
	ClientSentAt        *time.Time
	ServerReceivedAt    time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	EditedAt            *time.Time
	RecalledAt          *time.Time
	Attachments         []MessageAttachment
}

type MessageAttachment struct {
	ID          string
	MessageID   string
	File        File
	DisplayName *string
	DurationMS  *int
	Width       *int
	Height      *int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateMessageParams struct {
	ConversationID      string
	SenderID            string
	MessageSeq          int64
	Type                string
	ContentText         *string
	ContentJSON         json.RawMessage
	ReferencedMessageID *string
	ClientMessageID     string
	ClientSentAt        *time.Time
	Attachments         []CreateMessageAttachmentParams
}

type CreateMessageAttachmentParams struct {
	FileID      string
	DisplayName *string
	DurationMS  *int
	Width       *int
	Height      *int
}

func NewChatRepository(db DBTX) *ChatRepository {
	return &ChatRepository{db: db}
}

func (r *ChatRepository) LockConversationForParticipant(ctx context.Context, conversationID string, userID string) error {
	var id pgtype.UUID
	err := r.db.QueryRow(ctx, `
		SELECT c.id
		FROM conversations c
		JOIN conversation_participants cp
		  ON cp.conversation_id = c.id
		 AND cp.user_id = $2::uuid
		WHERE c.id = $1::uuid
		  AND c.deleted_at IS NULL
		FOR UPDATE OF c`,
		normalizeText(conversationID),
		normalizeText(userID),
	).Scan(&id)
	return translateReadError(err)
}

func (r *ChatRepository) ParticipantExists(ctx context.Context, conversationID string, userID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM conversations c
			JOIN conversation_participants cp
			  ON cp.conversation_id = c.id
			 AND cp.user_id = $2::uuid
			WHERE c.id = $1::uuid
			  AND c.deleted_at IS NULL
		)`,
		normalizeText(conversationID),
		normalizeText(userID),
	).Scan(&exists)
	return exists, translateReadError(err)
}

func (r *ChatRepository) NextMessageSeq(ctx context.Context, conversationID string) (int64, error) {
	var seq int64
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(MAX(message_seq), 0) + 1
		FROM messages
		WHERE conversation_id = $1::uuid`,
		normalizeText(conversationID),
	).Scan(&seq)
	return seq, translateReadError(err)
}

func (r *ChatRepository) CreateMessage(ctx context.Context, params CreateMessageParams) (Message, error) {
	contentJSON := jsonOrDefault(params.ContentJSON)
	message, err := scanMessage(r.db.QueryRow(ctx, `
		INSERT INTO messages (
			conversation_id,
			sender_id,
			message_seq,
			type,
			content_text,
			content_json,
			referenced_message_id,
			client_message_id,
			client_sent_at
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, NULLIF($7, '')::uuid, $8, $9)
		RETURNING `+messageSelectColumns,
		normalizeText(params.ConversationID),
		normalizeText(params.SenderID),
		params.MessageSeq,
		normalizeText(params.Type),
		normalizeTextPtr(params.ContentText),
		contentJSON,
		normalizeOptionalUUIDPtr(params.ReferencedMessageID),
		normalizeText(params.ClientMessageID),
		params.ClientSentAt,
	))
	if err != nil {
		return Message{}, translateWriteError(err)
	}

	for _, attachment := range params.Attachments {
		if _, err := r.db.Exec(ctx, `
			INSERT INTO message_attachments (
				message_id,
				file_id,
				display_name,
				duration_ms,
				width,
				height
			)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)`,
			message.ID,
			normalizeText(attachment.FileID),
			normalizeTextPtr(attachment.DisplayName),
			attachment.DurationMS,
			attachment.Width,
			attachment.Height,
		); err != nil {
			return Message{}, translateWriteError(err)
		}
	}

	return r.GetMessageForParticipant(ctx, params.SenderID, message.ID)
}

func (r *ChatRepository) TouchConversationAfterMessage(ctx context.Context, conversationID string, messageID string, senderID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE conversations
		SET last_message_id = $2::uuid,
		    last_message_at = now(),
		    version = version + 1,
		    updated_at = now()
		WHERE id = $1::uuid`,
		normalizeText(conversationID),
		normalizeText(messageID),
	)
	if err != nil {
		return translateWriteError(err)
	}

	_, err = r.db.Exec(ctx, `
		UPDATE conversation_participants
		SET unread_count = CASE
		        WHEN user_id = $2::uuid THEN unread_count
		        ELSE unread_count + 1
		    END,
		    updated_at = now()
		WHERE conversation_id = $1::uuid`,
		normalizeText(conversationID),
		normalizeText(senderID),
	)
	return translateWriteError(err)
}

func (r *ChatRepository) ListMessagesForParticipant(ctx context.Context, userID string, conversationID string, beforeSeq *int64, afterSeq *int64, limit int) ([]Message, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+messageSelectColumns+`
		FROM messages m
		JOIN conversation_participants cp
		  ON cp.conversation_id = m.conversation_id
		 AND cp.user_id = $2::uuid
		JOIN conversations c
		  ON c.id = m.conversation_id
		 AND c.deleted_at IS NULL
		WHERE m.conversation_id = $1::uuid
		  AND ($3::bigint IS NULL OR m.message_seq < $3)
		  AND ($4::bigint IS NULL OR m.message_seq > $4)
		ORDER BY m.message_seq DESC
		LIMIT $5`,
		normalizeText(conversationID),
		normalizeText(userID),
		beforeSeq,
		afterSeq,
		limit,
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	messages, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Message, error) {
		return scanMessage(row)
	})
	if err != nil {
		return nil, translateReadError(err)
	}
	if len(messages) == 0 {
		exists, existsErr := r.ParticipantExists(ctx, conversationID, userID)
		if existsErr != nil {
			return nil, existsErr
		}
		if !exists {
			return nil, ErrNotFound
		}
		return messages, nil
	}

	if err := r.loadAttachments(ctx, messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func (r *ChatRepository) GetMessageForParticipant(ctx context.Context, userID string, messageID string) (Message, error) {
	message, err := scanMessage(r.db.QueryRow(ctx, `
		SELECT `+messageSelectColumns+`
		FROM messages m
		JOIN conversation_participants cp
		  ON cp.conversation_id = m.conversation_id
		 AND cp.user_id = $2::uuid
		JOIN conversations c
		  ON c.id = m.conversation_id
		 AND c.deleted_at IS NULL
		WHERE m.id = $1::uuid`,
		normalizeText(messageID),
		normalizeText(userID),
	))
	if err != nil {
		return Message{}, translateReadError(err)
	}

	messages := []Message{message}
	if err := r.loadAttachments(ctx, messages); err != nil {
		return Message{}, err
	}
	return messages[0], nil
}

func (r *ChatRepository) GetMessageByClientMessageID(ctx context.Context, userID string, conversationID string, senderID string, clientMessageID string) (Message, error) {
	message, err := scanMessage(r.db.QueryRow(ctx, `
		SELECT `+messageSelectColumns+`
		FROM messages m
		JOIN conversation_participants cp
		  ON cp.conversation_id = m.conversation_id
		 AND cp.user_id = $1::uuid
		JOIN conversations c
		  ON c.id = m.conversation_id
		 AND c.deleted_at IS NULL
		WHERE m.conversation_id = $2::uuid
		  AND m.sender_id = $3::uuid
		  AND m.client_message_id = $4`,
		normalizeText(userID),
		normalizeText(conversationID),
		normalizeText(senderID),
		normalizeText(clientMessageID),
	))
	if err != nil {
		return Message{}, translateReadError(err)
	}

	messages := []Message{message}
	if err := r.loadAttachments(ctx, messages); err != nil {
		return Message{}, err
	}
	return messages[0], nil
}

func (r *ChatRepository) loadAttachments(ctx context.Context, messages []Message) error {
	messageIDs := make([]string, 0, len(messages))
	messageIndex := make(map[string]int, len(messages))
	for index, message := range messages {
		messageIDs = append(messageIDs, message.ID)
		messageIndex[message.ID] = index
	}

	rows, err := r.db.Query(ctx, `
		SELECT
			ma.id,
			ma.message_id,
			f.id,
			f.owner_user_id,
			f.original_file_name,
			f.bucket,
			f.object_key,
			f.mime_type,
			f.size_bytes,
			f.etag,
			f.content_hash,
			f.storage_status,
			f.visibility,
			f.version,
			f.created_at,
			f.updated_at,
			f.deleted_at,
			ma.display_name,
			ma.duration_ms,
			ma.width,
			ma.height,
			ma.created_at,
			ma.updated_at
		FROM message_attachments ma
		JOIN files f ON f.id = ma.file_id
		WHERE ma.message_id::text = ANY($1)
		ORDER BY ma.created_at ASC, ma.id ASC`,
		messageIDs,
	)
	if err != nil {
		return translateReadError(err)
	}
	defer rows.Close()

	for rows.Next() {
		attachment, err := scanMessageAttachment(rows)
		if err != nil {
			return translateReadError(err)
		}
		if index, ok := messageIndex[attachment.MessageID]; ok {
			messages[index].Attachments = append(messages[index].Attachments, attachment)
		}
	}
	return translateReadError(rows.Err())
}

func scanMessage(row scanner) (Message, error) {
	var (
		message             Message
		id                  pgtype.UUID
		conversationID      pgtype.UUID
		senderID            pgtype.UUID
		contentText         sql.NullString
		referencedMessageID pgtype.UUID
		clientSentAt        pgtype.Timestamptz
		editedAt            pgtype.Timestamptz
		recalledAt          pgtype.Timestamptz
	)

	err := row.Scan(
		&id,
		&conversationID,
		&senderID,
		&message.MessageSeq,
		&message.Type,
		&contentText,
		&message.ContentJSON,
		&referencedMessageID,
		&message.ClientMessageID,
		&clientSentAt,
		&message.ServerReceivedAt,
		&message.CreatedAt,
		&message.UpdatedAt,
		&editedAt,
		&recalledAt,
	)
	if err != nil {
		return Message{}, err
	}

	message.ID = uuidString(id)
	message.ConversationID = uuidString(conversationID)
	message.SenderID = uuidString(senderID)
	if contentText.Valid {
		message.ContentText = &contentText.String
	}
	message.ReferencedMessageID = uuidStringPtr(referencedMessageID)
	if clientSentAt.Valid {
		message.ClientSentAt = &clientSentAt.Time
	}
	if editedAt.Valid {
		message.EditedAt = &editedAt.Time
	}
	if recalledAt.Valid {
		message.RecalledAt = &recalledAt.Time
	}

	return message, nil
}

func scanMessageAttachment(row scanner) (MessageAttachment, error) {
	var (
		attachment  MessageAttachment
		id          pgtype.UUID
		messageID   pgtype.UUID
		fileID      pgtype.UUID
		ownerUserID pgtype.UUID
		etag        sql.NullString
		contentHash sql.NullString
		deletedAt   pgtype.Timestamptz
		displayName sql.NullString
		durationMS  sql.NullInt32
		width       sql.NullInt32
		height      sql.NullInt32
	)

	err := row.Scan(
		&id,
		&messageID,
		&fileID,
		&ownerUserID,
		&attachment.File.OriginalFileName,
		&attachment.File.Bucket,
		&attachment.File.ObjectKey,
		&attachment.File.MimeType,
		&attachment.File.SizeBytes,
		&etag,
		&contentHash,
		&attachment.File.StorageStatus,
		&attachment.File.Visibility,
		&attachment.File.Version,
		&attachment.File.CreatedAt,
		&attachment.File.UpdatedAt,
		&deletedAt,
		&displayName,
		&durationMS,
		&width,
		&height,
		&attachment.CreatedAt,
		&attachment.UpdatedAt,
	)
	if err != nil {
		return MessageAttachment{}, err
	}

	attachment.ID = uuidString(id)
	attachment.MessageID = uuidString(messageID)
	attachment.File.ID = uuidString(fileID)
	attachment.File.OwnerUserID = uuidStringPtr(ownerUserID)
	if etag.Valid {
		attachment.File.ETag = &etag.String
	}
	if contentHash.Valid {
		attachment.File.ContentHash = &contentHash.String
	}
	if deletedAt.Valid {
		attachment.File.DeletedAt = &deletedAt.Time
	}
	if displayName.Valid {
		attachment.DisplayName = &displayName.String
	}
	if durationMS.Valid {
		value := int(durationMS.Int32)
		attachment.DurationMS = &value
	}
	if width.Valid {
		value := int(width.Int32)
		attachment.Width = &value
	}
	if height.Valid {
		value := int(height.Int32)
		attachment.Height = &value
	}

	return attachment, nil
}

func normalizeOptionalUUIDPtr(value *string) string {
	if value == nil {
		return ""
	}
	return normalizeText(*value)
}
