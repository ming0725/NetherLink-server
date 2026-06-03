package repository

import (
	"context"
	"encoding/json"
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

const aiFileChunkSelectColumns = `
	id,
	ai_file_id,
	chunk_index,
	content,
	token_count,
	metadata_json,
	created_at,
	updated_at`

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

type AIFileChunk struct {
	ID         string
	AIFileID   string
	ChunkIndex int
	Content    string
	TokenCount int
	Metadata   json.RawMessage
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ExpiredAIUploadedFileCleanupCandidate struct {
	AIFileID            string
	FileID              string
	ExtractedTextFileID *string
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

type CreateAIFileChunkParams struct {
	AIFileID   string
	ChunkIndex int
	Content    string
	TokenCount int
	Metadata   json.RawMessage
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

func (r *AIRepository) ListUploadedFilesByFileID(ctx context.Context, fileID string) ([]AIUploadedFile, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+prefixedColumns(aiUploadedFileSelectColumns, "auf")+`
		FROM ai_uploaded_files auf
		JOIN ai_conversations c ON c.id = auf.conversation_id
		WHERE auf.file_id = $1::uuid
		  AND c.deleted_at IS NULL
		  AND auf.deleted_at IS NULL
		ORDER BY auf.created_at ASC, auf.id ASC`,
		normalizeText(fileID),
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

func (r *AIRepository) SetUploadedFileParseStatus(ctx context.Context, aiFileID string, status string, tokenCount int) (AIUploadedFile, error) {
	file, err := scanAIUploadedFile(r.db.QueryRow(ctx, `
		UPDATE ai_uploaded_files
		SET parse_status = $2,
		    token_count = $3,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND deleted_at IS NULL
		RETURNING `+aiUploadedFileSelectColumns,
		normalizeText(aiFileID),
		normalizeText(status),
		tokenCount,
	))
	return file, translateWriteError(err)
}

func (r *AIRepository) SetUploadedFileExtractedTextFile(ctx context.Context, aiFileID string, extractedTextFileID string) (AIUploadedFile, error) {
	file, err := scanAIUploadedFile(r.db.QueryRow(ctx, `
		UPDATE ai_uploaded_files
		SET extracted_text_file_id = $2::uuid,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND deleted_at IS NULL
		RETURNING `+aiUploadedFileSelectColumns,
		normalizeText(aiFileID),
		normalizeText(extractedTextFileID),
	))
	return file, translateWriteError(err)
}

func (r *AIRepository) SetUploadedFilesParseStatusByFileID(ctx context.Context, fileID string, status string, tokenCount int) error {
	_, err := r.db.Exec(ctx, `
		UPDATE ai_uploaded_files
		SET parse_status = $2,
		    token_count = $3,
		    updated_at = now()
		WHERE file_id = $1::uuid
		  AND deleted_at IS NULL`,
		normalizeText(fileID),
		normalizeText(status),
		tokenCount,
	)
	return err
}

func (r *AIRepository) ReplaceFileChunks(ctx context.Context, aiFileID string, chunks []CreateAIFileChunkParams) error {
	if _, err := r.db.Exec(ctx, `
		DELETE FROM ai_file_chunks
		WHERE ai_file_id = $1::uuid`,
		normalizeText(aiFileID),
	); err != nil {
		return err
	}
	for _, chunk := range chunks {
		metadata := jsonOrDefault(chunk.Metadata)
		if _, err := r.db.Exec(ctx, `
			INSERT INTO ai_file_chunks (
				ai_file_id,
				chunk_index,
				content,
				token_count,
				metadata_json
			)
			VALUES ($1::uuid, $2, $3, $4, $5)`,
			normalizeText(aiFileID),
			chunk.ChunkIndex,
			normalizeText(chunk.Content),
			chunk.TokenCount,
			metadata,
		); err != nil {
			return translateWriteError(err)
		}
	}
	return nil
}

func (r *AIRepository) ListFileChunksByUploadedFileIDs(ctx context.Context, userID string, conversationID string, aiFileIDs []string) ([]AIFileChunk, error) {
	if len(aiFileIDs) == 0 {
		return []AIFileChunk{}, nil
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
		return []AIFileChunk{}, nil
	}
	placeholders := make([]string, 0, len(normalizedIDs))
	args := make([]any, 0, len(normalizedIDs)+2)
	for i, aiFileID := range normalizedIDs {
		placeholders = append(placeholders, fmt.Sprintf("$%d::uuid", i+1))
		args = append(args, aiFileID)
	}
	args = append(args, normalizeText(conversationID), normalizeText(userID))
	rows, err := r.db.Query(ctx, `
		SELECT `+prefixedColumns(aiFileChunkSelectColumns, "chunk")+`
		FROM ai_file_chunks chunk
		JOIN ai_uploaded_files auf ON auf.id = chunk.ai_file_id
		JOIN ai_conversations c ON c.id = auf.conversation_id
		WHERE auf.id IN (`+strings.Join(placeholders, ", ")+`)
		  AND c.id = $`+fmt.Sprint(len(normalizedIDs)+1)+`::uuid
		  AND c.user_id = $`+fmt.Sprint(len(normalizedIDs)+2)+`::uuid
		  AND c.deleted_at IS NULL
		  AND auf.deleted_at IS NULL
		ORDER BY auf.created_at ASC, chunk.chunk_index ASC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chunks := []AIFileChunk{}
	for rows.Next() {
		chunk, err := scanAIFileChunk(rows)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
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
			WHERE fu.usage_type IN ('ai_upload', 'ai_extracted_text')
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

func (r *AIRepository) CleanupExpiredTemporaryUploadedFiles(ctx context.Context, limit int) ([]ExpiredAIUploadedFileCleanupCandidate, error) {
	if limit <= 0 {
		limit = 25
	}

	rows, err := r.db.Query(ctx, `
		WITH candidates AS (
			SELECT auf.id
			FROM ai_uploaded_files auf
			JOIN ai_conversations c ON c.id = auf.conversation_id
			WHERE auf.deleted_at IS NULL
			  AND c.deleted_at IS NULL
			  AND COALESCE(NULLIF(auf.retention_policy, ''), 'conversation') = 'temporary'
			  AND auf.expires_at IS NOT NULL
			  AND auf.expires_at < now()
			ORDER BY auf.expires_at ASC, auf.created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		),
		marked AS (
			UPDATE ai_uploaded_files auf
			SET deleted_at = now(),
			    updated_at = now()
			FROM candidates
			WHERE auf.id = candidates.id
			RETURNING auf.id, auf.file_id, auf.extracted_text_file_id
		),
		deleted_chunks AS (
			DELETE FROM ai_file_chunks chunk
			USING marked
			WHERE chunk.ai_file_id = marked.id
		),
		deleted_usages AS (
			UPDATE file_usages fu
			SET deleted_at = now(),
			    updated_at = now()
			FROM marked
			WHERE fu.usage_type IN ('ai_upload', 'ai_extracted_text')
			  AND fu.business_id = marked.id
			  AND fu.deleted_at IS NULL
		)
		SELECT id, file_id, extracted_text_file_id
		FROM marked
		ORDER BY id`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := []ExpiredAIUploadedFileCleanupCandidate{}
	for rows.Next() {
		var (
			aiFileID            pgtype.UUID
			fileID              pgtype.UUID
			extractedTextFileID pgtype.UUID
		)
		if err := rows.Scan(&aiFileID, &fileID, &extractedTextFileID); err != nil {
			return nil, err
		}
		candidates = append(candidates, ExpiredAIUploadedFileCleanupCandidate{
			AIFileID:            uuidString(aiFileID),
			FileID:              uuidString(fileID),
			ExtractedTextFileID: uuidStringPtr(extractedTextFileID),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
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

func scanAIFileChunk(row scanner) (AIFileChunk, error) {
	var (
		chunk    AIFileChunk
		id       pgtype.UUID
		aiFileID pgtype.UUID
	)
	err := row.Scan(
		&id,
		&aiFileID,
		&chunk.ChunkIndex,
		&chunk.Content,
		&chunk.TokenCount,
		&chunk.Metadata,
		&chunk.CreatedAt,
		&chunk.UpdatedAt,
	)
	if err != nil {
		return AIFileChunk{}, err
	}
	chunk.ID = uuidString(id)
	chunk.AIFileID = uuidString(aiFileID)
	return chunk, nil
}

func prefixedColumns(columns string, alias string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		parts[i] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}
