package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

const conversationSelectColumns = `
	c.id,
	c.type,
	c.peer_user_id,
	c.group_id,
	c.last_message_id,
	c.last_message_at,
	c.version,
	c.created_at,
	c.updated_at`

const participantSelectColumns = `
	cp.conversation_id,
	cp.user_id,
	cp.is_pinned,
	cp.is_dnd,
	cp.last_read_message_id,
	cp.last_read_at,
	cp.unread_count,
	cp.hidden_at,
	cp.created_at,
	cp.updated_at`

type ChatRepository struct {
	db DBTX
}

type Conversation struct {
	ID            string
	Type          string
	PeerUserID    *string
	GroupID       *string
	LastMessageID *string
	LastMessageAt *time.Time
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ConversationParticipant struct {
	ConversationID    string
	UserID            string
	IsPinned          bool
	IsDND             bool
	LastReadMessageID *string
	LastReadAt        *time.Time
	UnreadCount       int
	HiddenAt          *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ConversationUserSummary struct {
	ID           string
	PublicID     string
	Nick         string
	Gender       string
	GenderCustom *string
	AvatarFileID *string
	Status       string
	Signature    *string
	Region       *string
	LastSeenAt   *time.Time
	Version      int
	UpdatedAt    time.Time
}

type ConversationGroupSummary struct {
	ID           string
	Name         string
	OwnerID      string
	AvatarFileID *string
	Introduction *string
	Announcement *string
	MemberCount  int
	Version      int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ConversationEntry struct {
	Conversation Conversation
	Participant  ConversationParticipant
	PeerUser     *ConversationUserSummary
	Group        *ConversationGroupSummary
	LastMessage  *Message
}

type CreateConversationParams struct {
	Type       string
	PeerUserID *string
	GroupID    *string
}

type UpdateConversationSettingsParams struct {
	UserID         string
	ConversationID string
	IsPinned       *bool
	IsDND          *bool
	Hidden         *bool
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

func (r *ChatRepository) ListConversationsForUser(ctx context.Context, userID string, keyword string, limit int, offset int) ([]ConversationEntry, error) {
	rows, err := r.db.Query(ctx, conversationEntrySelectSQL()+`
		WHERE cp.user_id = $1::uuid
		  AND c.deleted_at IS NULL
		  AND cp.hidden_at IS NULL
		  AND (
		    $2 = ''
		    OR peer.public_id ILIKE '%' || $2 || '%'
		    OR peer.nick ILIKE '%' || $2 || '%'
		    OR g.name ILIKE '%' || $2 || '%'
		  )
		ORDER BY cp.is_pinned DESC, COALESCE(c.last_message_at, c.updated_at) DESC, c.created_at DESC
		LIMIT $3 OFFSET $4`,
		normalizeText(userID),
		normalizeText(keyword),
		limit,
		offset,
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ConversationEntry, error) {
		return scanConversationEntry(row)
	})
	if err != nil {
		return nil, translateReadError(err)
	}
	if err := r.loadEntryLastMessageAttachments(ctx, entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (r *ChatRepository) GetConversationForUser(ctx context.Context, userID string, conversationID string) (ConversationEntry, error) {
	entry, err := scanConversationEntry(r.db.QueryRow(ctx, conversationEntrySelectSQL()+`
		WHERE cp.user_id = $1::uuid
		  AND c.id = $2::uuid
		  AND c.deleted_at IS NULL`,
		normalizeText(userID),
		normalizeText(conversationID),
	))
	if err != nil {
		return ConversationEntry{}, translateReadError(err)
	}
	entries := []ConversationEntry{entry}
	if err := r.loadEntryLastMessageAttachments(ctx, entries); err != nil {
		return ConversationEntry{}, err
	}
	return entries[0], nil
}

func (r *ChatRepository) FindDirectConversation(ctx context.Context, userID string, peerUserID string) (string, bool, error) {
	var id pgtype.UUID
	err := r.db.QueryRow(ctx, `
		SELECT c.id
		FROM conversations c
		JOIN conversation_participants self_cp
		  ON self_cp.conversation_id = c.id
		 AND self_cp.user_id = $1::uuid
		JOIN conversation_participants peer_cp
		  ON peer_cp.conversation_id = c.id
		 AND peer_cp.user_id = $2::uuid
		WHERE c.type = 'direct'
		  AND c.deleted_at IS NULL
		ORDER BY c.created_at ASC
		LIMIT 1`,
		normalizeText(userID),
		normalizeText(peerUserID),
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, translateReadError(err)
	}
	return uuidString(id), true, nil
}

func (r *ChatRepository) FindGroupConversation(ctx context.Context, groupID string) (string, bool, error) {
	var id pgtype.UUID
	err := r.db.QueryRow(ctx, `
		SELECT id
		FROM conversations
		WHERE type = 'group'
		  AND group_id = $1::uuid
		  AND deleted_at IS NULL
		ORDER BY created_at ASC
		LIMIT 1`,
		normalizeText(groupID),
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, translateReadError(err)
	}
	return uuidString(id), true, nil
}

func (r *ChatRepository) CreateConversation(ctx context.Context, params CreateConversationParams) (Conversation, error) {
	conversation, err := scanConversation(r.db.QueryRow(ctx, `
		INSERT INTO conversations (
			type,
			peer_user_id,
			group_id
		)
		VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid)
		RETURNING `+conversationSelectColumns,
		normalizeText(params.Type),
		normalizeOptionalUUIDPtr(params.PeerUserID),
		normalizeOptionalUUIDPtr(params.GroupID),
	))
	return conversation, translateWriteError(err)
}

func (r *ChatRepository) AddConversationParticipants(ctx context.Context, conversationID string, userIDs []string) error {
	for _, userID := range userIDs {
		if _, err := r.db.Exec(ctx, `
			INSERT INTO conversation_participants (
				conversation_id,
				user_id
			)
			VALUES ($1::uuid, $2::uuid)
			ON CONFLICT (conversation_id, user_id) DO NOTHING`,
			normalizeText(conversationID),
			normalizeText(userID),
		); err != nil {
			return translateWriteError(err)
		}
	}
	return nil
}

func (r *ChatRepository) ListGroupMemberUserIDs(ctx context.Context, groupID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT user_id
		FROM group_members
		WHERE group_id = $1::uuid
		ORDER BY joined_at ASC`,
		normalizeText(groupID),
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	userIDs := make([]string, 0)
	for rows.Next() {
		var userID pgtype.UUID
		if err := rows.Scan(&userID); err != nil {
			return nil, translateReadError(err)
		}
		userIDs = append(userIDs, uuidString(userID))
	}
	return userIDs, translateReadError(rows.Err())
}

func (r *ChatRepository) ListConversationParticipantUserIDs(ctx context.Context, conversationID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT cp.user_id
		FROM conversation_participants cp
		JOIN conversations c
		  ON c.id = cp.conversation_id
		 AND c.deleted_at IS NULL
		WHERE cp.conversation_id = $1::uuid
		ORDER BY cp.created_at ASC`,
		normalizeText(conversationID),
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	userIDs := make([]string, 0)
	for rows.Next() {
		var userID pgtype.UUID
		if err := rows.Scan(&userID); err != nil {
			return nil, translateReadError(err)
		}
		userIDs = append(userIDs, uuidString(userID))
	}
	return userIDs, translateReadError(rows.Err())
}

func (r *ChatRepository) ListConversationParticipants(ctx context.Context, conversationID string) ([]ConversationParticipant, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+participantSelectColumns+`
		FROM conversation_participants cp
		JOIN conversations c
		  ON c.id = cp.conversation_id
		 AND c.deleted_at IS NULL
		WHERE cp.conversation_id = $1::uuid
		ORDER BY cp.created_at ASC`,
		normalizeText(conversationID),
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	participants, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ConversationParticipant, error) {
		return scanConversationParticipant(row)
	})
	return participants, translateReadError(err)
}

func (r *ChatRepository) UpdateConversationSettings(ctx context.Context, params UpdateConversationSettingsParams) (ConversationParticipant, error) {
	participant, err := scanConversationParticipant(r.db.QueryRow(ctx, `
		UPDATE conversation_participants cp
		SET is_pinned = CASE WHEN $3 THEN $4 ELSE is_pinned END,
		    is_dnd = CASE WHEN $5 THEN $6 ELSE is_dnd END,
		    hidden_at = CASE WHEN $7 THEN CASE WHEN $8 THEN now() ELSE NULL END ELSE hidden_at END,
		    updated_at = now()
		FROM conversations c
		WHERE c.id = cp.conversation_id
		  AND cp.user_id = $1::uuid
		  AND cp.conversation_id = $2::uuid
		  AND c.deleted_at IS NULL
		RETURNING `+participantSelectColumns,
		normalizeText(params.UserID),
		normalizeText(params.ConversationID),
		params.IsPinned != nil,
		boolValue(params.IsPinned),
		params.IsDND != nil,
		boolValue(params.IsDND),
		params.Hidden != nil,
		boolValue(params.Hidden),
	))
	return participant, translateWriteError(err)
}

func (r *ChatRepository) MarkConversationRead(ctx context.Context, userID string, conversationID string, lastReadMessageID *string, lastReadAt *time.Time) (ConversationParticipant, error) {
	participant, err := scanConversationParticipant(r.db.QueryRow(ctx, `
		UPDATE conversation_participants cp
		SET last_read_message_id = COALESCE(NULLIF($3, '')::uuid, c.last_message_id),
		    last_read_at = COALESCE($4, now()),
		    unread_count = 0,
		    updated_at = now()
		FROM conversations c
		WHERE c.id = cp.conversation_id
		  AND cp.user_id = $1::uuid
		  AND cp.conversation_id = $2::uuid
		  AND c.deleted_at IS NULL
		  AND (
		    $3 = ''
		    OR EXISTS (
		      SELECT 1
		      FROM messages m
		      WHERE m.id = NULLIF($3, '')::uuid
		        AND m.conversation_id = cp.conversation_id
		    )
		  )
		RETURNING `+participantSelectColumns,
		normalizeText(userID),
		normalizeText(conversationID),
		normalizeOptionalUUIDPtr(lastReadMessageID),
		lastReadAt,
	))
	return participant, translateWriteError(err)
}

func (r *ChatRepository) MarkConversationUnread(ctx context.Context, userID string, conversationID string, lastReadMessageID *string, unreadCount *int) (ConversationParticipant, error) {
	participant, err := scanConversationParticipant(r.db.QueryRow(ctx, `
		UPDATE conversation_participants cp
		SET last_read_message_id = CASE WHEN $3 THEN NULLIF($4, '')::uuid ELSE last_read_message_id END,
		    unread_count = CASE WHEN $5 THEN $6 ELSE GREATEST(unread_count, 1) END,
		    updated_at = now()
		FROM conversations c
		WHERE c.id = cp.conversation_id
		  AND cp.user_id = $1::uuid
		  AND cp.conversation_id = $2::uuid
		  AND c.deleted_at IS NULL
		  AND (
		    $4 = ''
		    OR EXISTS (
		      SELECT 1
		      FROM messages m
		      WHERE m.id = NULLIF($4, '')::uuid
		        AND m.conversation_id = cp.conversation_id
		    )
		  )
		RETURNING `+participantSelectColumns,
		normalizeText(userID),
		normalizeText(conversationID),
		lastReadMessageID != nil,
		normalizeOptionalUUIDPtr(lastReadMessageID),
		unreadCount != nil,
		intValue(unreadCount),
	))
	return participant, translateWriteError(err)
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
		    hidden_at = NULL,
		    updated_at = now()
		WHERE conversation_id = $1::uuid`,
		normalizeText(conversationID),
		normalizeText(senderID),
	)
	return translateWriteError(err)
}

func (r *ChatRepository) RecallMessage(ctx context.Context, userID string, conversationID string, messageID string) (Message, error) {
	message, err := scanMessage(r.db.QueryRow(ctx, `
		UPDATE messages m
		SET recalled_at = COALESCE(m.recalled_at, now()),
		    updated_at = now()
		FROM conversations c
		JOIN conversation_participants cp
		  ON cp.conversation_id = c.id
		 AND cp.user_id = $1::uuid
		WHERE m.id = $3::uuid
		  AND m.conversation_id = $2::uuid
		  AND m.sender_id = $1::uuid
		  AND c.id = m.conversation_id
		  AND c.deleted_at IS NULL
		RETURNING `+messageSelectColumns,
		normalizeText(userID),
		normalizeText(conversationID),
		normalizeText(messageID),
	))
	if err != nil {
		return Message{}, translateWriteError(err)
	}

	if _, err := r.db.Exec(ctx, `
		UPDATE conversations
		SET version = version + 1,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND last_message_id = $2::uuid`,
		normalizeText(conversationID),
		normalizeText(messageID),
	); err != nil {
		return Message{}, translateWriteError(err)
	}

	messages := []Message{message}
	if err := r.loadAttachments(ctx, messages); err != nil {
		return Message{}, err
	}
	return messages[0], nil
}

func (r *ChatRepository) HideConversationHistory(ctx context.Context, userID string, conversationID string) (ConversationParticipant, error) {
	participant, err := scanConversationParticipant(r.db.QueryRow(ctx, `
		UPDATE conversation_participants cp
		SET hidden_at = now(),
		    last_read_message_id = c.last_message_id,
		    last_read_at = now(),
		    unread_count = 0,
		    updated_at = now()
		FROM conversations c
		WHERE c.id = cp.conversation_id
		  AND cp.user_id = $1::uuid
		  AND cp.conversation_id = $2::uuid
		  AND c.deleted_at IS NULL
		RETURNING `+participantSelectColumns,
		normalizeText(userID),
		normalizeText(conversationID),
	))
	return participant, translateWriteError(err)
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
		  AND (cp.hidden_at IS NULL OR m.created_at > cp.hidden_at)
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

func conversationEntrySelectSQL() string {
	return `
		SELECT
			c.id,
			c.type,
			c.peer_user_id,
			c.group_id,
			c.last_message_id,
			c.last_message_at,
			c.version,
			c.created_at,
			c.updated_at,
			cp.conversation_id,
			cp.user_id,
			cp.is_pinned,
			cp.is_dnd,
			cp.last_read_message_id,
			cp.last_read_at,
			cp.unread_count,
			cp.hidden_at,
			cp.created_at,
			cp.updated_at,
			peer.id,
			peer.public_id,
			peer.nick,
			peer.gender,
			peer.gender_custom,
			peer.avatar_file_id,
			peer.status,
			peer.signature,
			peer.region,
			peer.last_seen_at,
			peer.version,
			peer.updated_at,
			g.id,
			g.name,
			g.owner_id,
			g.avatar_file_id,
			g.introduction,
			g.announcement,
			(
			  SELECT count(*)::int
			  FROM group_members member_count
			  WHERE member_count.group_id = g.id
			) AS member_count,
			g.version,
			g.created_at,
			g.updated_at,
			lm.id,
			lm.conversation_id,
			lm.sender_id,
			lm.message_seq,
			lm.type,
			lm.content_text,
			lm.content_json,
			lm.referenced_message_id,
			lm.client_message_id,
			lm.client_sent_at,
			lm.server_received_at,
			lm.created_at,
			lm.updated_at,
			lm.edited_at,
			lm.recalled_at
		FROM conversation_participants cp
		JOIN conversations c ON c.id = cp.conversation_id
		LEFT JOIN LATERAL (
			SELECT u.id,
			       u.public_id,
			       u.nick,
			       u.gender,
			       u.gender_custom,
			       u.avatar_file_id,
			       u.status,
			       u.signature,
			       u.region,
			       u.last_seen_at,
			       u.version,
			       u.updated_at
			FROM conversation_participants peer_cp
			JOIN users u ON u.id = peer_cp.user_id
			WHERE c.type = 'direct'
			  AND peer_cp.conversation_id = c.id
			  AND peer_cp.user_id <> cp.user_id
			  AND u.deleted_at IS NULL
			ORDER BY u.created_at ASC
			LIMIT 1
		) peer ON true
		LEFT JOIN groups g
		  ON g.id = c.group_id
		 AND g.deleted_at IS NULL
		LEFT JOIN messages lm ON lm.id = c.last_message_id`
}

func scanConversationEntry(row scanner) (ConversationEntry, error) {
	var (
		entry ConversationEntry

		conversationID pgtype.UUID
		peerUserID     pgtype.UUID
		groupID        pgtype.UUID
		lastMessageID  pgtype.UUID
		lastMessageAt  pgtype.Timestamptz

		participantConversationID pgtype.UUID
		participantUserID         pgtype.UUID
		lastReadMessageID         pgtype.UUID
		lastReadAt                pgtype.Timestamptz
		hiddenAt                  pgtype.Timestamptz

		peerID           pgtype.UUID
		peerPublicID     sql.NullString
		peerNick         sql.NullString
		peerGender       sql.NullString
		peerGenderCustom sql.NullString
		peerAvatarFileID pgtype.UUID
		peerStatus       sql.NullString
		peerSignature    sql.NullString
		peerRegion       sql.NullString
		peerLastSeenAt   pgtype.Timestamptz
		peerVersion      sql.NullInt32
		peerUpdatedAt    pgtype.Timestamptz

		entryGroupID           pgtype.UUID
		groupName              sql.NullString
		groupOwnerID           pgtype.UUID
		groupAvatarFileID      pgtype.UUID
		groupIntroduction      sql.NullString
		groupAnnouncement      sql.NullString
		groupMemberCount       sql.NullInt32
		groupVersion           sql.NullInt32
		groupCreatedAt         pgtype.Timestamptz
		groupUpdatedAt         pgtype.Timestamptz
		lastMessage            Message
		lastMessageContentText sql.NullString
		lastMessageContentJSON []byte
		lastMessageIDValue     pgtype.UUID
		lastMessageConvID      pgtype.UUID
		lastMessageSenderID    pgtype.UUID
		lastMessageSeq         sql.NullInt64
		lastMessageType        sql.NullString
		lastReferencedID       pgtype.UUID
		lastClientMessageID    sql.NullString
		lastClientSentAt       pgtype.Timestamptz
		lastServerReceivedAt   pgtype.Timestamptz
		lastCreatedAt          pgtype.Timestamptz
		lastUpdatedAt          pgtype.Timestamptz
		lastEditedAt           pgtype.Timestamptz
		lastRecalledAt         pgtype.Timestamptz
	)

	err := row.Scan(
		&conversationID,
		&entry.Conversation.Type,
		&peerUserID,
		&groupID,
		&lastMessageID,
		&lastMessageAt,
		&entry.Conversation.Version,
		&entry.Conversation.CreatedAt,
		&entry.Conversation.UpdatedAt,
		&participantConversationID,
		&participantUserID,
		&entry.Participant.IsPinned,
		&entry.Participant.IsDND,
		&lastReadMessageID,
		&lastReadAt,
		&entry.Participant.UnreadCount,
		&hiddenAt,
		&entry.Participant.CreatedAt,
		&entry.Participant.UpdatedAt,
		&peerID,
		&peerPublicID,
		&peerNick,
		&peerGender,
		&peerGenderCustom,
		&peerAvatarFileID,
		&peerStatus,
		&peerSignature,
		&peerRegion,
		&peerLastSeenAt,
		&peerVersion,
		&peerUpdatedAt,
		&entryGroupID,
		&groupName,
		&groupOwnerID,
		&groupAvatarFileID,
		&groupIntroduction,
		&groupAnnouncement,
		&groupMemberCount,
		&groupVersion,
		&groupCreatedAt,
		&groupUpdatedAt,
		&lastMessageIDValue,
		&lastMessageConvID,
		&lastMessageSenderID,
		&lastMessageSeq,
		&lastMessageType,
		&lastMessageContentText,
		&lastMessageContentJSON,
		&lastReferencedID,
		&lastClientMessageID,
		&lastClientSentAt,
		&lastServerReceivedAt,
		&lastCreatedAt,
		&lastUpdatedAt,
		&lastEditedAt,
		&lastRecalledAt,
	)
	if err != nil {
		return ConversationEntry{}, err
	}

	entry.Conversation.ID = uuidString(conversationID)
	entry.Conversation.PeerUserID = uuidStringPtr(peerUserID)
	entry.Conversation.GroupID = uuidStringPtr(groupID)
	entry.Conversation.LastMessageID = uuidStringPtr(lastMessageID)
	if lastMessageAt.Valid {
		entry.Conversation.LastMessageAt = &lastMessageAt.Time
	}
	entry.Participant.ConversationID = uuidString(participantConversationID)
	entry.Participant.UserID = uuidString(participantUserID)
	entry.Participant.LastReadMessageID = uuidStringPtr(lastReadMessageID)
	if lastReadAt.Valid {
		entry.Participant.LastReadAt = &lastReadAt.Time
	}
	if hiddenAt.Valid {
		entry.Participant.HiddenAt = &hiddenAt.Time
	}

	if peerID.Valid {
		peer := ConversationUserSummary{
			ID:           uuidString(peerID),
			PublicID:     peerPublicID.String,
			Nick:         peerNick.String,
			Gender:       peerGender.String,
			AvatarFileID: uuidStringPtr(peerAvatarFileID),
			Status:       peerStatus.String,
			Version:      int(peerVersion.Int32),
		}
		if peerGenderCustom.Valid {
			peer.GenderCustom = &peerGenderCustom.String
		}
		if peerSignature.Valid {
			peer.Signature = &peerSignature.String
		}
		if peerRegion.Valid {
			peer.Region = &peerRegion.String
		}
		if peerLastSeenAt.Valid {
			peer.LastSeenAt = &peerLastSeenAt.Time
		}
		if peerUpdatedAt.Valid {
			peer.UpdatedAt = peerUpdatedAt.Time
		}
		entry.PeerUser = &peer
	}

	if entryGroupID.Valid {
		group := ConversationGroupSummary{
			ID:           uuidString(entryGroupID),
			Name:         groupName.String,
			OwnerID:      uuidString(groupOwnerID),
			AvatarFileID: uuidStringPtr(groupAvatarFileID),
			MemberCount:  int(groupMemberCount.Int32),
			Version:      int(groupVersion.Int32),
		}
		if groupIntroduction.Valid {
			group.Introduction = &groupIntroduction.String
		}
		if groupAnnouncement.Valid {
			group.Announcement = &groupAnnouncement.String
		}
		if groupCreatedAt.Valid {
			group.CreatedAt = groupCreatedAt.Time
		}
		if groupUpdatedAt.Valid {
			group.UpdatedAt = groupUpdatedAt.Time
		}
		entry.Group = &group
	}

	if lastMessageIDValue.Valid {
		lastMessage.ID = uuidString(lastMessageIDValue)
		lastMessage.ConversationID = uuidString(lastMessageConvID)
		lastMessage.SenderID = uuidString(lastMessageSenderID)
		lastMessage.MessageSeq = lastMessageSeq.Int64
		lastMessage.Type = lastMessageType.String
		if lastMessageContentText.Valid {
			lastMessage.ContentText = &lastMessageContentText.String
		}
		if len(lastMessageContentJSON) > 0 {
			lastMessage.ContentJSON = json.RawMessage(lastMessageContentJSON)
		}
		lastMessage.ReferencedMessageID = uuidStringPtr(lastReferencedID)
		lastMessage.ClientMessageID = lastClientMessageID.String
		if lastClientSentAt.Valid {
			lastMessage.ClientSentAt = &lastClientSentAt.Time
		}
		if lastServerReceivedAt.Valid {
			lastMessage.ServerReceivedAt = lastServerReceivedAt.Time
		}
		if lastCreatedAt.Valid {
			lastMessage.CreatedAt = lastCreatedAt.Time
		}
		if lastUpdatedAt.Valid {
			lastMessage.UpdatedAt = lastUpdatedAt.Time
		}
		if lastEditedAt.Valid {
			lastMessage.EditedAt = &lastEditedAt.Time
		}
		if lastRecalledAt.Valid {
			lastMessage.RecalledAt = &lastRecalledAt.Time
		}
		entry.LastMessage = &lastMessage
	}

	return entry, nil
}

func scanConversation(row scanner) (Conversation, error) {
	var (
		conversation  Conversation
		id            pgtype.UUID
		peerUserID    pgtype.UUID
		groupID       pgtype.UUID
		lastMessageID pgtype.UUID
		lastMessageAt pgtype.Timestamptz
	)

	err := row.Scan(
		&id,
		&conversation.Type,
		&peerUserID,
		&groupID,
		&lastMessageID,
		&lastMessageAt,
		&conversation.Version,
		&conversation.CreatedAt,
		&conversation.UpdatedAt,
	)
	if err != nil {
		return Conversation{}, err
	}
	conversation.ID = uuidString(id)
	conversation.PeerUserID = uuidStringPtr(peerUserID)
	conversation.GroupID = uuidStringPtr(groupID)
	conversation.LastMessageID = uuidStringPtr(lastMessageID)
	if lastMessageAt.Valid {
		conversation.LastMessageAt = &lastMessageAt.Time
	}
	return conversation, nil
}

func scanConversationParticipant(row scanner) (ConversationParticipant, error) {
	var (
		participant       ConversationParticipant
		conversationID    pgtype.UUID
		userID            pgtype.UUID
		lastReadMessageID pgtype.UUID
		lastReadAt        pgtype.Timestamptz
		hiddenAt          pgtype.Timestamptz
	)

	err := row.Scan(
		&conversationID,
		&userID,
		&participant.IsPinned,
		&participant.IsDND,
		&lastReadMessageID,
		&lastReadAt,
		&participant.UnreadCount,
		&hiddenAt,
		&participant.CreatedAt,
		&participant.UpdatedAt,
	)
	if err != nil {
		return ConversationParticipant{}, err
	}
	participant.ConversationID = uuidString(conversationID)
	participant.UserID = uuidString(userID)
	participant.LastReadMessageID = uuidStringPtr(lastReadMessageID)
	if lastReadAt.Valid {
		participant.LastReadAt = &lastReadAt.Time
	}
	if hiddenAt.Valid {
		participant.HiddenAt = &hiddenAt.Time
	}
	return participant, nil
}

func (r *ChatRepository) loadEntryLastMessageAttachments(ctx context.Context, entries []ConversationEntry) error {
	messages := make([]Message, 0, len(entries))
	positions := make(map[string]int, len(entries))
	for _, entry := range entries {
		if entry.LastMessage == nil {
			continue
		}
		positions[entry.LastMessage.ID] = len(messages)
		messages = append(messages, *entry.LastMessage)
	}
	if len(messages) == 0 {
		return nil
	}
	if err := r.loadAttachments(ctx, messages); err != nil {
		return err
	}
	for index := range entries {
		if entries[index].LastMessage == nil {
			continue
		}
		if position, ok := positions[entries[index].LastMessage.ID]; ok {
			entries[index].LastMessage = &messages[position]
		}
	}
	return nil
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func normalizeOptionalUUIDPtr(value *string) string {
	if value == nil {
		return ""
	}
	return normalizeText(*value)
}
