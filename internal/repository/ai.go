package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const aiConversationSelectColumns = `
	id,
	user_id,
	title,
	last_message_at,
	has_unread_dot,
	model,
	context_used_tokens,
	context_max_tokens,
	created_at,
	updated_at,
	deleted_at`

const aiMessageSelectColumns = `
	id,
	conversation_id,
	role,
	text,
	token_count,
	feedback,
	created_at,
	updated_at,
	deleted_at`

const aiUploadedFileSelectColumns = `
	id,
	conversation_id,
	message_id,
	file_id,
	purpose,
	parse_status,
	extracted_text_file_id,
	token_count,
	retention_policy,
	expires_at,
	created_at,
	updated_at,
	deleted_at`

type AIRepository struct {
	db DBTX
}

type AIConversation struct {
	ID                string
	UserID            string
	Title             *string
	LastMessageAt     *time.Time
	HasUnreadDot      bool
	Model             *string
	ContextUsedTokens int
	ContextMaxTokens  int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

type AIMessage struct {
	ID             string
	ConversationID string
	Role           string
	Text           string
	TokenCount     int
	Feedback       *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

type AIUploadedFile struct {
	ID                  string
	ConversationID      string
	MessageID           *string
	FileID              string
	Purpose             string
	ParseStatus         string
	ExtractedTextFileID *string
	TokenCount          int
	RetentionPolicy     *string
	ExpiresAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time
}

type CreateAIConversationParams struct {
	UserID           string
	Title            *string
	Model            string
	ContextMaxTokens int
}

type CreateAIMessageParams struct {
	ConversationID string
	Role           string
	Text           string
	TokenCount     int
}

type CreateAIUploadedFileParams struct {
	ConversationID  string
	FileID          string
	Purpose         string
	RetentionPolicy *string
	ExpiresAt       *time.Time
}

func NewAIRepository(db DBTX) *AIRepository {
	return &AIRepository{db: db}
}

func (r *AIRepository) ListConversations(ctx context.Context, userID string, limit int, offset int) ([]AIConversation, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+aiConversationSelectColumns+`
		FROM ai_conversations
		WHERE user_id = $1::uuid
		  AND deleted_at IS NULL
		ORDER BY COALESCE(last_message_at, created_at) DESC, created_at DESC
		LIMIT $2 OFFSET $3`,
		normalizeText(userID),
		limit,
		offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conversations := []AIConversation{}
	for rows.Next() {
		conversation, err := scanAIConversation(rows)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return conversations, nil
}

func (r *AIRepository) CreateConversation(ctx context.Context, params CreateAIConversationParams) (AIConversation, error) {
	conversation, err := scanAIConversation(r.db.QueryRow(ctx, `
		INSERT INTO ai_conversations (
			user_id,
			title,
			model,
			context_max_tokens
		)
		VALUES ($1::uuid, $2, $3, $4)
		RETURNING `+aiConversationSelectColumns,
		normalizeText(params.UserID),
		normalizeTextPtr(params.Title),
		normalizeText(params.Model),
		params.ContextMaxTokens,
	))
	return conversation, translateWriteError(err)
}

func (r *AIRepository) GetConversation(ctx context.Context, userID string, conversationID string) (AIConversation, error) {
	conversation, err := scanAIConversation(r.db.QueryRow(ctx, `
		SELECT `+aiConversationSelectColumns+`
		FROM ai_conversations
		WHERE id = $1::uuid
		  AND user_id = $2::uuid
		  AND deleted_at IS NULL`,
		normalizeText(conversationID),
		normalizeText(userID),
	))
	return conversation, translateReadError(err)
}

func (r *AIRepository) UpdateConversationTitle(ctx context.Context, userID string, conversationID string, title string) (AIConversation, error) {
	conversation, err := scanAIConversation(r.db.QueryRow(ctx, `
		UPDATE ai_conversations
		SET title = $3
		WHERE id = $1::uuid
		  AND user_id = $2::uuid
		  AND deleted_at IS NULL
		RETURNING `+aiConversationSelectColumns,
		normalizeText(conversationID),
		normalizeText(userID),
		normalizeText(title),
	))
	return conversation, translateWriteError(err)
}

func (r *AIRepository) CreateMessage(ctx context.Context, params CreateAIMessageParams) (AIMessage, error) {
	message, err := scanAIMessage(r.db.QueryRow(ctx, `
		INSERT INTO ai_messages (
			conversation_id,
			role,
			text,
			token_count
		)
		VALUES ($1::uuid, $2, $3, $4)
		RETURNING `+aiMessageSelectColumns,
		normalizeText(params.ConversationID),
		normalizeText(params.Role),
		normalizeText(params.Text),
		params.TokenCount,
	))
	if err != nil {
		return AIMessage{}, translateWriteError(err)
	}
	if _, err := r.db.Exec(ctx, `
		UPDATE ai_conversations
		SET last_message_at = $2,
		    context_used_tokens = context_used_tokens + $3
		WHERE id = $1::uuid
		  AND deleted_at IS NULL`,
		normalizeText(params.ConversationID),
		message.CreatedAt,
		params.TokenCount,
	); err != nil {
		return AIMessage{}, err
	}
	return message, nil
}

func (r *AIRepository) ListMessages(ctx context.Context, userID string, conversationID string) ([]AIMessage, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+prefixedColumns(aiMessageSelectColumns, "m")+`
		FROM ai_messages m
		JOIN ai_conversations c ON c.id = m.conversation_id
		WHERE c.id = $1::uuid
		  AND c.user_id = $2::uuid
		  AND c.deleted_at IS NULL
		  AND m.deleted_at IS NULL
		ORDER BY m.created_at ASC, m.id ASC`,
		normalizeText(conversationID),
		normalizeText(userID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := []AIMessage{}
	for rows.Next() {
		message, err := scanAIMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func (r *AIRepository) SetMessageFeedback(ctx context.Context, userID string, messageID string, feedback string) (AIMessage, error) {
	message, err := scanAIMessage(r.db.QueryRow(ctx, `
		UPDATE ai_messages m
		SET feedback = NULLIF($3, 'none')
		FROM ai_conversations c
		WHERE m.id = $1::uuid
		  AND c.id = m.conversation_id
		  AND c.user_id = $2::uuid
		  AND c.deleted_at IS NULL
		  AND m.deleted_at IS NULL
		RETURNING `+prefixedColumns(aiMessageSelectColumns, "m"),
		normalizeText(messageID),
		normalizeText(userID),
		normalizeText(feedback),
	))
	return message, translateWriteError(err)
}

func (r *AIRepository) CreateUploadedFile(ctx context.Context, params CreateAIUploadedFileParams) (AIUploadedFile, error) {
	uploadedFile, err := scanAIUploadedFile(r.db.QueryRow(ctx, `
		INSERT INTO ai_uploaded_files (
			conversation_id,
			file_id,
			purpose,
			parse_status,
			retention_policy,
			expires_at
		)
		VALUES ($1::uuid, $2::uuid, $3, 'pending', $4, $5)
		RETURNING `+aiUploadedFileSelectColumns,
		normalizeText(params.ConversationID),
		normalizeText(params.FileID),
		normalizeText(params.Purpose),
		normalizeTextPtr(params.RetentionPolicy),
		params.ExpiresAt,
	))
	return uploadedFile, translateWriteError(err)
}

func (r *AIRepository) ListUploadedFiles(ctx context.Context, userID string, conversationID string) ([]AIUploadedFile, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+prefixedColumns(aiUploadedFileSelectColumns, "auf")+`
		FROM ai_uploaded_files auf
		JOIN ai_conversations c ON c.id = auf.conversation_id
		WHERE c.id = $1::uuid
		  AND c.user_id = $2::uuid
		  AND c.deleted_at IS NULL
		  AND auf.deleted_at IS NULL
		ORDER BY auf.created_at ASC, auf.id ASC`,
		normalizeText(conversationID),
		normalizeText(userID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := []AIUploadedFile{}
	for rows.Next() {
		file, err := scanAIUploadedFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

func (r *AIRepository) GetUploadedFile(ctx context.Context, userID string, aiFileID string) (AIUploadedFile, error) {
	file, err := scanAIUploadedFile(r.db.QueryRow(ctx, `
		SELECT `+prefixedColumns(aiUploadedFileSelectColumns, "auf")+`
		FROM ai_uploaded_files auf
		JOIN ai_conversations c ON c.id = auf.conversation_id
		WHERE auf.id = $1::uuid
		  AND c.user_id = $2::uuid
		  AND c.deleted_at IS NULL
		  AND auf.deleted_at IS NULL`,
		normalizeText(aiFileID),
		normalizeText(userID),
	))
	return file, translateReadError(err)
}

func (r *AIRepository) ListUploadedFilesByIDs(ctx context.Context, userID string, conversationID string, aiFileIDs []string) ([]AIUploadedFile, error) {
	if len(aiFileIDs) == 0 {
		return []AIUploadedFile{}, nil
	}
	normalizedIDs := make([]string, 0, len(aiFileIDs))
	seen := map[string]bool{}
	for _, aiFileID := range aiFileIDs {
		normalized := normalizeText(aiFileID)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		normalizedIDs = append(normalizedIDs, normalized)
	}
	if len(normalizedIDs) == 0 {
		return []AIUploadedFile{}, nil
	}
	placeholders := make([]string, 0, len(normalizedIDs))
	args := make([]any, 0, len(normalizedIDs)+2)
	for i, aiFileID := range normalizedIDs {
		placeholders = append(placeholders, fmt.Sprintf("$%d::uuid", i+1))
		args = append(args, aiFileID)
	}
	args = append(args, normalizeText(conversationID), normalizeText(userID))
	rows, err := r.db.Query(ctx, `
		SELECT `+prefixedColumns(aiUploadedFileSelectColumns, "auf")+`
		FROM ai_uploaded_files auf
		JOIN ai_conversations c ON c.id = auf.conversation_id
		WHERE auf.id IN (`+strings.Join(placeholders, ", ")+`)
		  AND c.id = $`+fmt.Sprint(len(normalizedIDs)+1)+`::uuid
		  AND c.user_id = $`+fmt.Sprint(len(normalizedIDs)+2)+`::uuid
		  AND c.deleted_at IS NULL
		  AND auf.deleted_at IS NULL
		ORDER BY auf.created_at ASC, auf.id ASC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := []AIUploadedFile{}
	for rows.Next() {
		file, err := scanAIUploadedFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

func (r *AIRepository) DeleteUploadedFile(ctx context.Context, userID string, aiFileID string) error {
	var deletedCount int
	err := r.db.QueryRow(ctx, `
		WITH deleted AS (
			UPDATE ai_uploaded_files auf
			SET deleted_at = now()
			FROM ai_conversations c
			WHERE auf.id = $1::uuid
			  AND c.id = auf.conversation_id
			  AND c.user_id = $2::uuid
			  AND c.deleted_at IS NULL
			  AND auf.deleted_at IS NULL
			RETURNING auf.id
		),
		usage_deleted AS (
			UPDATE file_usages fu
			SET deleted_at = now()
			FROM deleted
			WHERE fu.usage_type = 'ai_upload'
			  AND fu.business_id = deleted.id
			  AND fu.deleted_at IS NULL
			RETURNING fu.id
		)
		SELECT count(*) FROM deleted`,
		normalizeText(aiFileID),
		normalizeText(userID),
	).Scan(&deletedCount)
	if err != nil {
		return err
	}
	if deletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func scanAIConversation(row scanner) (AIConversation, error) {
	var (
		conversation AIConversation
		id           pgtype.UUID
		userID       pgtype.UUID
	)
	err := row.Scan(
		&id,
		&userID,
		&conversation.Title,
		&conversation.LastMessageAt,
		&conversation.HasUnreadDot,
		&conversation.Model,
		&conversation.ContextUsedTokens,
		&conversation.ContextMaxTokens,
		&conversation.CreatedAt,
		&conversation.UpdatedAt,
		&conversation.DeletedAt,
	)
	if err != nil {
		return AIConversation{}, err
	}
	conversation.ID = uuidString(id)
	conversation.UserID = uuidString(userID)
	return conversation, nil
}

func scanAIMessage(row scanner) (AIMessage, error) {
	var (
		message        AIMessage
		id             pgtype.UUID
		conversationID pgtype.UUID
	)
	err := row.Scan(
		&id,
		&conversationID,
		&message.Role,
		&message.Text,
		&message.TokenCount,
		&message.Feedback,
		&message.CreatedAt,
		&message.UpdatedAt,
		&message.DeletedAt,
	)
	if err != nil {
		return AIMessage{}, err
	}
	message.ID = uuidString(id)
	message.ConversationID = uuidString(conversationID)
	return message, nil
}

func scanAIUploadedFile(row scanner) (AIUploadedFile, error) {
	var (
		file                AIUploadedFile
		id                  pgtype.UUID
		conversationID      pgtype.UUID
		messageID           pgtype.UUID
		fileID              pgtype.UUID
		extractedTextFileID pgtype.UUID
	)
	err := row.Scan(
		&id,
		&conversationID,
		&messageID,
		&fileID,
		&file.Purpose,
		&file.ParseStatus,
		&extractedTextFileID,
		&file.TokenCount,
		&file.RetentionPolicy,
		&file.ExpiresAt,
		&file.CreatedAt,
		&file.UpdatedAt,
		&file.DeletedAt,
	)
	if err != nil {
		return AIUploadedFile{}, err
	}
	file.ID = uuidString(id)
	file.ConversationID = uuidString(conversationID)
	file.MessageID = uuidStringPtr(messageID)
	file.FileID = uuidString(fileID)
	file.ExtractedTextFileID = uuidStringPtr(extractedTextFileID)
	return file, nil
}

func prefixedColumns(columns string, alias string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		parts[i] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}
