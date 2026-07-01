package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const agentProfileSelectColumns = `
	id,
	bot_user_id,
	owner_user_id,
	kind,
	visibility,
	name,
	avatar_file_id,
	base_prompt,
	model,
	tool_policy_json,
	safety_policy_json,
	style_profile_id,
	status,
	version,
	created_at,
	updated_at,
	deleted_at`

const agentProfileSelectColumnsAP = `
	ap.id,
	ap.bot_user_id,
	ap.owner_user_id,
	ap.kind,
	ap.visibility,
	ap.name,
	ap.avatar_file_id,
	ap.base_prompt,
	ap.model,
	ap.tool_policy_json,
	ap.safety_policy_json,
	ap.style_profile_id,
	ap.status,
	ap.version,
	ap.created_at,
	ap.updated_at,
	ap.deleted_at`

type AgentRepository struct {
	db DBTX
}

type AgentProfile struct {
	ID             string
	BotUserID      string
	OwnerUserID    *string
	Kind           string
	Visibility     string
	Name           string
	AvatarFileID   *string
	BasePrompt     string
	Model          *string
	ToolPolicy     json.RawMessage
	SafetyPolicy   json.RawMessage
	StyleProfileID *string
	Status         string
	Version        int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

type CreateAgentBotUserParams struct {
	Name         string
	AvatarFileID *string
}

type CreateAgentProfileParams struct {
	BotUserID      string
	OwnerUserID    *string
	Kind           string
	Visibility     string
	Name           string
	AvatarFileID   *string
	BasePrompt     string
	Model          *string
	ToolPolicy     json.RawMessage
	SafetyPolicy   json.RawMessage
	StyleProfileID *string
}

type CreateAgentRunParams struct {
	AgentID           string
	InvokerUserID     *string
	SourceType        string
	SourceID          *string
	InputSnapshot     json.RawMessage
	ContextRefs       json.RawMessage
	ClientOperationID *string
}

type UpdateAgentRunResultParams struct {
	RunID            string
	Status           string
	ToolCalls        json.RawMessage
	Output           json.RawMessage
	Model            *string
	PromptTokens     int
	CompletionTokens int
	LatencyMS        int
	ErrorCode        *string
}

type CreateAgentBindingParams struct {
	AgentID         string
	ScopeType       string
	ScopeID         string
	TriggerMode     string
	Enabled         bool
	CooldownSeconds int
	CreatedBy       string
}

type CreateAgentMemoryParams struct {
	AgentID    string
	UserID     *string
	ScopeType  string
	ScopeID    *string
	MemoryType string
	Content    string
	SourceRefs json.RawMessage
	Confidence float64
}

type CreateRAGDocumentParams struct {
	SourceType  string
	SourceID    string
	OwnerUserID *string
	Visibility  string
	Title       *string
	Metadata    json.RawMessage
}

type CreateRAGChunkParams struct {
	DocumentID string
	ChunkIndex int
	Content    string
	TokenCount int
	Metadata   json.RawMessage
}

type AgentBinding struct {
	ID              string
	AgentID         string
	ScopeType       string
	ScopeID         string
	TriggerMode     string
	Enabled         bool
	CooldownSeconds int
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

type AgentRun struct {
	ID                string
	AgentID           string
	InvokerUserID     *string
	SourceType        string
	SourceID          *string
	Status            string
	InputSnapshot     json.RawMessage
	ContextRefs       json.RawMessage
	ToolCalls         json.RawMessage
	Output            json.RawMessage
	Model             *string
	PromptTokens      int
	CompletionTokens  int
	LatencyMS         int
	ErrorCode         *string
	ClientOperationID *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

type AgentMemory struct {
	ID         string
	AgentID    string
	UserID     *string
	ScopeType  string
	ScopeID    *string
	MemoryType string
	Content    string
	SourceRefs json.RawMessage
	Confidence float64
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time
}

type RAGDocument struct {
	ID          string
	SourceType  string
	SourceID    string
	OwnerUserID *string
	Visibility  string
	Title       *string
	Metadata    json.RawMessage
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

type RAGChunk struct {
	ID         string
	DocumentID string
	ChunkIndex int
	Content    string
	TokenCount int
	Metadata   json.RawMessage
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func NewAgentRepository(db DBTX) *AgentRepository {
	return &AgentRepository{db: db}
}

func (r *AgentRepository) CreateBotUser(ctx context.Context, params CreateAgentBotUserParams) (string, error) {
	var botUserID pgtype.UUID
	err := r.db.QueryRow(ctx, `
		WITH generated AS (
			SELECT gen_random_uuid() AS id
		)
		INSERT INTO users (
			id,
			public_id,
			email,
			password_hash,
			nick,
			gender,
			avatar_file_id,
			account_type,
			status
		)
		SELECT
			id,
			'agent_' || substr(replace(id::text, '-', ''), 1, 16),
			'agent_' || replace(id::text, '-', '') || '@agents.local',
			'agent-no-password',
			$1,
			'unspecified',
			$2::uuid,
			'agent',
			'active'
		FROM generated
		RETURNING id`,
		normalizeText(params.Name),
		normalizeTextPtr(params.AvatarFileID),
	).Scan(&botUserID)
	if err != nil {
		return "", translateWriteError(err)
	}
	return uuidString(botUserID), nil
}

func (r *AgentRepository) CreateProfile(ctx context.Context, params CreateAgentProfileParams) (AgentProfile, error) {
	profile, err := scanAgentProfile(r.db.QueryRow(ctx, `
		INSERT INTO agent_profiles (
			bot_user_id,
			owner_user_id,
			kind,
			visibility,
			name,
			avatar_file_id,
			base_prompt,
			model,
			tool_policy_json,
			safety_policy_json,
			style_profile_id
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::uuid, $7, $8, $9, $10, $11::uuid)
		RETURNING `+agentProfileSelectColumns,
		normalizeText(params.BotUserID),
		normalizeTextPtr(params.OwnerUserID),
		normalizeText(params.Kind),
		normalizeText(params.Visibility),
		normalizeText(params.Name),
		normalizeTextPtr(params.AvatarFileID),
		normalizeText(params.BasePrompt),
		normalizeTextPtr(params.Model),
		jsonOrDefault(params.ToolPolicy),
		jsonOrDefault(params.SafetyPolicy),
		normalizeTextPtr(params.StyleProfileID),
	))
	return profile, translateWriteError(err)
}

func (r *AgentRepository) CreateBinding(ctx context.Context, params CreateAgentBindingParams) (AgentBinding, error) {
	binding, err := scanAgentBinding(r.db.QueryRow(ctx, `
		INSERT INTO agent_bindings (
			agent_id,
			scope_type,
			scope_id,
			trigger_mode,
			enabled,
			cooldown_seconds,
			created_by
		)
		VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7::uuid)
		ON CONFLICT (agent_id, scope_type, scope_id) DO UPDATE
		SET trigger_mode = EXCLUDED.trigger_mode,
		    enabled = EXCLUDED.enabled,
		    cooldown_seconds = EXCLUDED.cooldown_seconds,
		    updated_at = now(),
		    deleted_at = NULL
		RETURNING
			id,
			agent_id,
			scope_type,
			scope_id,
			trigger_mode,
			enabled,
			cooldown_seconds,
			created_by,
			created_at,
			updated_at,
			deleted_at`,
		normalizeText(params.AgentID),
		normalizeText(params.ScopeType),
		normalizeText(params.ScopeID),
		normalizeText(params.TriggerMode),
		params.Enabled,
		params.CooldownSeconds,
		normalizeText(params.CreatedBy),
	))
	return binding, translateWriteError(err)
}

func (r *AgentRepository) ListProfilesForUser(ctx context.Context, userID string, limit int, offset int) ([]AgentProfile, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+agentProfileSelectColumns+`
		FROM agent_profiles
		WHERE deleted_at IS NULL
		  AND status = 'active'
		  AND (
		    kind = 'official'
		    OR owner_user_id = $1::uuid
		    OR visibility = 'public'
		  )
		ORDER BY
		  CASE WHEN kind = 'official' THEN 0 ELSE 1 END,
		  updated_at DESC,
		  created_at DESC
		LIMIT $2 OFFSET $3`,
		normalizeText(userID),
		limit,
		offset,
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	profiles := []AgentProfile{}
	for rows.Next() {
		profile, err := scanAgentProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, translateReadError(err)
	}
	return profiles, nil
}

func (r *AgentRepository) GetProfileForUser(ctx context.Context, userID string, agentID string) (AgentProfile, error) {
	profile, err := scanAgentProfile(r.db.QueryRow(ctx, `
		SELECT `+agentProfileSelectColumns+`
		FROM agent_profiles
		WHERE id = $2::uuid
		  AND deleted_at IS NULL
		  AND (
		    kind = 'official'
		    OR owner_user_id = $1::uuid
		    OR visibility = 'public'
		  )`,
		normalizeText(userID),
		normalizeText(agentID),
	))
	return profile, translateReadError(err)
}

func (r *AgentRepository) GetActiveGroupAgentByIdentifier(ctx context.Context, groupID string, identifier string) (AgentProfile, AgentBinding, error) {
	row := r.db.QueryRow(ctx, `
		SELECT
			`+agentProfileSelectColumnsAP+`,
			ab.id,
			ab.agent_id,
			ab.scope_type,
			ab.scope_id,
			ab.trigger_mode,
			ab.enabled,
			ab.cooldown_seconds,
			ab.created_by,
			ab.created_at,
			ab.updated_at,
			ab.deleted_at
		FROM agent_bindings ab
		JOIN agent_profiles ap
		  ON ap.id = ab.agent_id
		 AND ap.deleted_at IS NULL
		 AND ap.status = 'active'
		JOIN users bot
		  ON bot.id = ap.bot_user_id
		 AND bot.deleted_at IS NULL
		 AND bot.status = 'active'
		WHERE ab.scope_type = 'group'
		  AND ab.scope_id = $1::uuid
		  AND ab.trigger_mode = 'mention'
		  AND ab.enabled = true
		  AND ab.deleted_at IS NULL
		  AND (
		    ap.id::text = $2
		    OR ap.bot_user_id::text = $2
		    OR bot.public_id = $2
		  )
		LIMIT 1`,
		normalizeText(groupID),
		normalizeText(identifier),
	)
	profile, binding, err := scanAgentProfileWithBinding(row)
	return profile, binding, translateReadError(err)
}

func (r *AgentRepository) SoftDeleteProfile(ctx context.Context, ownerUserID string, agentID string) error {
	var deletedID pgtype.UUID
	err := r.db.QueryRow(ctx, `
		UPDATE agent_profiles
		SET deleted_at = now(),
		    updated_at = now(),
		    status = 'disabled'
		WHERE id = $2::uuid
		  AND owner_user_id = $1::uuid
		  AND deleted_at IS NULL
		RETURNING id`,
		normalizeText(ownerUserID),
		normalizeText(agentID),
	).Scan(&deletedID)
	return translateWriteError(err)
}

func (r *AgentRepository) CreateRun(ctx context.Context, params CreateAgentRunParams) (AgentRun, error) {
	run, err := scanAgentRun(r.db.QueryRow(ctx, `
		INSERT INTO agent_runs (
			agent_id,
			invoker_user_id,
			source_type,
			source_id,
			input_snapshot_json,
			context_refs_json,
			client_operation_id
		)
		VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7)
		RETURNING
			id,
			agent_id,
			invoker_user_id,
			source_type,
			source_id,
			status,
			input_snapshot_json,
			context_refs_json,
			tool_calls_json,
			output_json,
			model,
			prompt_tokens,
			completion_tokens,
			latency_ms,
			error_code,
			client_operation_id,
			created_at,
			updated_at,
			deleted_at`,
		normalizeText(params.AgentID),
		normalizeTextPtr(params.InvokerUserID),
		normalizeText(params.SourceType),
		normalizeTextPtr(params.SourceID),
		jsonOrDefault(params.InputSnapshot),
		jsonOrDefault(params.ContextRefs),
		normalizeTextPtr(params.ClientOperationID),
	))
	return run, translateWriteError(err)
}

func (r *AgentRepository) UpdateRunResult(ctx context.Context, params UpdateAgentRunResultParams) (AgentRun, error) {
	run, err := scanAgentRun(r.db.QueryRow(ctx, `
		UPDATE agent_runs
		SET status = $2,
		    tool_calls_json = $3,
		    output_json = $4,
		    model = $5,
		    prompt_tokens = $6,
		    completion_tokens = $7,
		    latency_ms = $8,
		    error_code = $9,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND deleted_at IS NULL
		RETURNING
			id,
			agent_id,
			invoker_user_id,
			source_type,
			source_id,
			status,
			input_snapshot_json,
			context_refs_json,
			tool_calls_json,
			output_json,
			model,
			prompt_tokens,
			completion_tokens,
			latency_ms,
			error_code,
			client_operation_id,
			created_at,
			updated_at,
			deleted_at`,
		normalizeText(params.RunID),
		normalizeText(params.Status),
		jsonOrDefaultArray(params.ToolCalls),
		jsonOrDefault(params.Output),
		normalizeTextPtr(params.Model),
		params.PromptTokens,
		params.CompletionTokens,
		params.LatencyMS,
		normalizeTextPtr(params.ErrorCode),
	))
	return run, translateWriteError(err)
}

func (r *AgentRepository) CreateMemory(ctx context.Context, params CreateAgentMemoryParams) (AgentMemory, error) {
	memory, err := scanAgentMemory(r.db.QueryRow(ctx, `
		INSERT INTO agent_memories (
			agent_id,
			user_id,
			scope_type,
			scope_id,
			memory_type,
			content,
			source_refs_json,
			confidence
		)
		VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7, $8)
		RETURNING
			id,
			agent_id,
			user_id,
			scope_type,
			scope_id,
			memory_type,
			content,
			source_refs_json,
			confidence,
			status,
			created_at,
			updated_at,
			deleted_at`,
		normalizeText(params.AgentID),
		normalizeTextPtr(params.UserID),
		normalizeText(params.ScopeType),
		normalizeTextPtr(params.ScopeID),
		normalizeText(params.MemoryType),
		normalizeText(params.Content),
		jsonOrDefault(params.SourceRefs),
		params.Confidence,
	))
	return memory, translateWriteError(err)
}

func (r *AgentRepository) ListGroupBotMemories(ctx context.Context, agentID string, groupID string, userID string, limit int) ([]AgentMemory, error) {
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT
			id,
			agent_id,
			user_id,
			scope_type,
			scope_id,
			memory_type,
			content,
			source_refs_json,
			confidence,
			status,
			created_at,
			updated_at,
			deleted_at
		FROM agent_memories
		WHERE agent_id = $1::uuid
		  AND user_id = $3::uuid
		  AND status = 'active'
		  AND deleted_at IS NULL
		  AND (
		    (scope_type = 'user' AND scope_id IS NULL)
		    OR (scope_type = 'group' AND scope_id = $2::uuid)
		  )
		ORDER BY confidence DESC, updated_at DESC, created_at DESC
		LIMIT $4`,
		normalizeText(agentID),
		normalizeText(groupID),
		normalizeText(userID),
		limit,
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	memories := []AgentMemory{}
	for rows.Next() {
		memory, err := scanAgentMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return memories, nil
}

func (r *AgentRepository) CreateRAGDocument(ctx context.Context, params CreateRAGDocumentParams) (RAGDocument, error) {
	document, err := scanRAGDocument(r.db.QueryRow(ctx, `
		INSERT INTO rag_documents (
			source_type,
			source_id,
			owner_user_id,
			visibility,
			title,
			metadata_json
		)
		VALUES ($1, $2::uuid, $3::uuid, $4, $5, $6)
		ON CONFLICT (source_type, source_id) DO UPDATE
		SET owner_user_id = EXCLUDED.owner_user_id,
		    visibility = EXCLUDED.visibility,
		    title = EXCLUDED.title,
		    metadata_json = EXCLUDED.metadata_json,
		    version = rag_documents.version + 1,
		    updated_at = now(),
		    deleted_at = NULL
		RETURNING
			id,
			source_type,
			source_id,
			owner_user_id,
			visibility,
			title,
			metadata_json,
			version,
			created_at,
			updated_at,
			deleted_at`,
		normalizeText(params.SourceType),
		normalizeText(params.SourceID),
		normalizeTextPtr(params.OwnerUserID),
		normalizeText(params.Visibility),
		normalizeTextPtr(params.Title),
		jsonOrDefault(params.Metadata),
	))
	return document, translateWriteError(err)
}

func (r *AgentRepository) CreateRAGChunk(ctx context.Context, params CreateRAGChunkParams) (RAGChunk, error) {
	chunk, err := scanRAGChunk(r.db.QueryRow(ctx, `
		INSERT INTO rag_chunks (
			document_id,
			chunk_index,
			content,
			token_count,
			metadata_json
		)
		VALUES ($1::uuid, $2, $3, $4, $5)
		ON CONFLICT (document_id, chunk_index) DO UPDATE
		SET content = EXCLUDED.content,
		    token_count = EXCLUDED.token_count,
		    metadata_json = EXCLUDED.metadata_json,
		    updated_at = now()
		RETURNING
			id,
			document_id,
			chunk_index,
			content,
			token_count,
			metadata_json,
			created_at,
			updated_at`,
		normalizeText(params.DocumentID),
		params.ChunkIndex,
		normalizeText(params.Content),
		params.TokenCount,
		jsonOrDefault(params.Metadata),
	))
	return chunk, translateWriteError(err)
}

func scanAgentProfile(row scanner) (AgentProfile, error) {
	var (
		id             pgtype.UUID
		botUserID      pgtype.UUID
		ownerUserID    pgtype.UUID
		avatarFileID   pgtype.UUID
		styleProfileID pgtype.UUID
		deletedAt      pgtype.Timestamptz
		profile        AgentProfile
	)
	err := row.Scan(
		&id,
		&botUserID,
		&ownerUserID,
		&profile.Kind,
		&profile.Visibility,
		&profile.Name,
		&avatarFileID,
		&profile.BasePrompt,
		&profile.Model,
		&profile.ToolPolicy,
		&profile.SafetyPolicy,
		&styleProfileID,
		&profile.Status,
		&profile.Version,
		&profile.CreatedAt,
		&profile.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return AgentProfile{}, translateReadError(err)
	}
	profile.ID = uuidString(id)
	profile.BotUserID = uuidString(botUserID)
	profile.OwnerUserID = uuidStringPtr(ownerUserID)
	profile.AvatarFileID = uuidStringPtr(avatarFileID)
	profile.ToolPolicy = jsonOrDefault(profile.ToolPolicy)
	profile.SafetyPolicy = jsonOrDefault(profile.SafetyPolicy)
	profile.StyleProfileID = uuidStringPtr(styleProfileID)
	if deletedAt.Valid {
		profile.DeletedAt = &deletedAt.Time
	}
	return profile, nil
}

func scanAgentProfileWithBinding(row scanner) (AgentProfile, AgentBinding, error) {
	var (
		id             pgtype.UUID
		botUserID      pgtype.UUID
		ownerUserID    pgtype.UUID
		avatarFileID   pgtype.UUID
		styleProfileID pgtype.UUID
		profileDeleted pgtype.Timestamptz
		profile        AgentProfile

		bindingID      pgtype.UUID
		bindingAgentID pgtype.UUID
		bindingScopeID pgtype.UUID
		createdBy      pgtype.UUID
		bindingDeleted pgtype.Timestamptz
		binding        AgentBinding
	)
	err := row.Scan(
		&id,
		&botUserID,
		&ownerUserID,
		&profile.Kind,
		&profile.Visibility,
		&profile.Name,
		&avatarFileID,
		&profile.BasePrompt,
		&profile.Model,
		&profile.ToolPolicy,
		&profile.SafetyPolicy,
		&styleProfileID,
		&profile.Status,
		&profile.Version,
		&profile.CreatedAt,
		&profile.UpdatedAt,
		&profileDeleted,
		&bindingID,
		&bindingAgentID,
		&binding.ScopeType,
		&bindingScopeID,
		&binding.TriggerMode,
		&binding.Enabled,
		&binding.CooldownSeconds,
		&createdBy,
		&binding.CreatedAt,
		&binding.UpdatedAt,
		&bindingDeleted,
	)
	if err != nil {
		return AgentProfile{}, AgentBinding{}, err
	}
	profile.ID = uuidString(id)
	profile.BotUserID = uuidString(botUserID)
	profile.OwnerUserID = uuidStringPtr(ownerUserID)
	profile.AvatarFileID = uuidStringPtr(avatarFileID)
	profile.ToolPolicy = jsonOrDefault(profile.ToolPolicy)
	profile.SafetyPolicy = jsonOrDefault(profile.SafetyPolicy)
	profile.StyleProfileID = uuidStringPtr(styleProfileID)
	if profileDeleted.Valid {
		profile.DeletedAt = &profileDeleted.Time
	}
	binding.ID = uuidString(bindingID)
	binding.AgentID = uuidString(bindingAgentID)
	binding.ScopeID = uuidString(bindingScopeID)
	binding.CreatedBy = uuidString(createdBy)
	if bindingDeleted.Valid {
		binding.DeletedAt = &bindingDeleted.Time
	}
	return profile, binding, nil
}

func scanAgentRun(row scanner) (AgentRun, error) {
	var (
		id            pgtype.UUID
		agentID       pgtype.UUID
		invokerUserID pgtype.UUID
		sourceID      pgtype.UUID
		deletedAt     pgtype.Timestamptz
		run           AgentRun
	)
	err := row.Scan(
		&id,
		&agentID,
		&invokerUserID,
		&run.SourceType,
		&sourceID,
		&run.Status,
		&run.InputSnapshot,
		&run.ContextRefs,
		&run.ToolCalls,
		&run.Output,
		&run.Model,
		&run.PromptTokens,
		&run.CompletionTokens,
		&run.LatencyMS,
		&run.ErrorCode,
		&run.ClientOperationID,
		&run.CreatedAt,
		&run.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return AgentRun{}, translateReadError(err)
	}
	run.ID = uuidString(id)
	run.AgentID = uuidString(agentID)
	run.InvokerUserID = uuidStringPtr(invokerUserID)
	run.SourceID = uuidStringPtr(sourceID)
	run.InputSnapshot = jsonOrDefault(run.InputSnapshot)
	run.ContextRefs = jsonOrDefault(run.ContextRefs)
	run.ToolCalls = jsonOrDefaultArray(run.ToolCalls)
	run.Output = jsonOrDefault(run.Output)
	if deletedAt.Valid {
		run.DeletedAt = &deletedAt.Time
	}
	return run, nil
}

func scanAgentBinding(row scanner) (AgentBinding, error) {
	var (
		id        pgtype.UUID
		agentID   pgtype.UUID
		scopeID   pgtype.UUID
		createdBy pgtype.UUID
		deletedAt pgtype.Timestamptz
		binding   AgentBinding
	)
	err := row.Scan(
		&id,
		&agentID,
		&binding.ScopeType,
		&scopeID,
		&binding.TriggerMode,
		&binding.Enabled,
		&binding.CooldownSeconds,
		&createdBy,
		&binding.CreatedAt,
		&binding.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return AgentBinding{}, translateReadError(err)
	}
	binding.ID = uuidString(id)
	binding.AgentID = uuidString(agentID)
	binding.ScopeID = uuidString(scopeID)
	binding.CreatedBy = uuidString(createdBy)
	if deletedAt.Valid {
		binding.DeletedAt = &deletedAt.Time
	}
	return binding, nil
}

func scanAgentMemory(row scanner) (AgentMemory, error) {
	var (
		id        pgtype.UUID
		agentID   pgtype.UUID
		userID    pgtype.UUID
		scopeID   pgtype.UUID
		deletedAt pgtype.Timestamptz
		memory    AgentMemory
	)
	err := row.Scan(
		&id,
		&agentID,
		&userID,
		&memory.ScopeType,
		&scopeID,
		&memory.MemoryType,
		&memory.Content,
		&memory.SourceRefs,
		&memory.Confidence,
		&memory.Status,
		&memory.CreatedAt,
		&memory.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return AgentMemory{}, translateReadError(err)
	}
	memory.ID = uuidString(id)
	memory.AgentID = uuidString(agentID)
	memory.UserID = uuidStringPtr(userID)
	memory.ScopeID = uuidStringPtr(scopeID)
	memory.SourceRefs = jsonOrDefault(memory.SourceRefs)
	if deletedAt.Valid {
		memory.DeletedAt = &deletedAt.Time
	}
	return memory, nil
}

func scanRAGDocument(row scanner) (RAGDocument, error) {
	var (
		id          pgtype.UUID
		sourceID    pgtype.UUID
		ownerUserID pgtype.UUID
		deletedAt   pgtype.Timestamptz
		document    RAGDocument
	)
	err := row.Scan(
		&id,
		&document.SourceType,
		&sourceID,
		&ownerUserID,
		&document.Visibility,
		&document.Title,
		&document.Metadata,
		&document.Version,
		&document.CreatedAt,
		&document.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return RAGDocument{}, translateReadError(err)
	}
	document.ID = uuidString(id)
	document.SourceID = uuidString(sourceID)
	document.OwnerUserID = uuidStringPtr(ownerUserID)
	document.Metadata = jsonOrDefault(document.Metadata)
	if deletedAt.Valid {
		document.DeletedAt = &deletedAt.Time
	}
	return document, nil
}

func scanRAGChunk(row scanner) (RAGChunk, error) {
	var (
		id         pgtype.UUID
		documentID pgtype.UUID
		chunk      RAGChunk
	)
	err := row.Scan(
		&id,
		&documentID,
		&chunk.ChunkIndex,
		&chunk.Content,
		&chunk.TokenCount,
		&chunk.Metadata,
		&chunk.CreatedAt,
		&chunk.UpdatedAt,
	)
	if err != nil {
		return RAGChunk{}, translateReadError(err)
	}
	chunk.ID = uuidString(id)
	chunk.DocumentID = uuidString(documentID)
	chunk.Metadata = jsonOrDefault(chunk.Metadata)
	return chunk, nil
}
