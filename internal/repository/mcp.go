package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const mcpServerSelectColumns = `
	id,
	name,
	display_name,
	transport,
	endpoint,
	command,
	args_json,
	env_json,
	secret_refs_json,
	status,
	created_at,
	updated_at,
	deleted_at`

const mcpToolSelectColumns = `
	id,
	server_id,
	name,
	namespaced_name,
	description,
	input_schema_json,
	output_schema_json,
	side_effect,
	requires_confirmation,
	status,
	last_synced_at,
	created_at,
	updated_at,
	deleted_at`

type MCPRepository struct {
	db DBTX
}

type MCPServer struct {
	ID          string
	Name        string
	DisplayName string
	Transport   string
	Endpoint    *string
	Command     *string
	Args        json.RawMessage
	Env         json.RawMessage
	SecretRefs  json.RawMessage
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

type MCPTool struct {
	ID                   string
	ServerID             string
	Name                 string
	NamespacedName       string
	Description          string
	InputSchema          json.RawMessage
	OutputSchema         json.RawMessage
	SideEffect           bool
	RequiresConfirmation bool
	Status               string
	LastSyncedAt         *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            *time.Time
}

type UpsertMCPServerParams struct {
	Name        string
	DisplayName string
	Transport   string
	Endpoint    *string
	Command     *string
	Args        json.RawMessage
	Env         json.RawMessage
	SecretRefs  json.RawMessage
	Status      string
}

type UpsertMCPToolParams struct {
	ServerID             string
	Name                 string
	NamespacedName       string
	Description          string
	InputSchema          json.RawMessage
	OutputSchema         json.RawMessage
	SideEffect           bool
	RequiresConfirmation bool
	Status               string
}

func NewMCPRepository(db DBTX) *MCPRepository {
	return &MCPRepository{db: db}
}

func (r *MCPRepository) UpsertServer(ctx context.Context, params UpsertMCPServerParams) (MCPServer, error) {
	server, err := scanMCPServer(r.db.QueryRow(ctx, `
		INSERT INTO mcp_servers (
			name,
			display_name,
			transport,
			endpoint,
			command,
			args_json,
			env_json,
			secret_refs_json,
			status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (name) DO UPDATE
		SET display_name = EXCLUDED.display_name,
		    transport = EXCLUDED.transport,
		    endpoint = EXCLUDED.endpoint,
		    command = EXCLUDED.command,
		    args_json = EXCLUDED.args_json,
		    env_json = EXCLUDED.env_json,
		    secret_refs_json = EXCLUDED.secret_refs_json,
		    status = EXCLUDED.status,
		    updated_at = now(),
		    deleted_at = NULL
		RETURNING `+mcpServerSelectColumns,
		normalizeText(params.Name),
		normalizeText(params.DisplayName),
		normalizeText(params.Transport),
		normalizeTextPtr(params.Endpoint),
		normalizeTextPtr(params.Command),
		jsonOrDefaultArray(params.Args),
		jsonOrDefault(params.Env),
		jsonOrDefault(params.SecretRefs),
		normalizeText(params.Status),
	))
	return server, translateWriteError(err)
}

func (r *MCPRepository) UpsertTool(ctx context.Context, params UpsertMCPToolParams) (MCPTool, error) {
	tool, err := scanMCPTool(r.db.QueryRow(ctx, `
		INSERT INTO mcp_tools (
			server_id,
			name,
			namespaced_name,
			description,
			input_schema_json,
			output_schema_json,
			side_effect,
			requires_confirmation,
			status,
			last_synced_at
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (server_id, name) DO UPDATE
		SET namespaced_name = EXCLUDED.namespaced_name,
		    description = EXCLUDED.description,
		    input_schema_json = EXCLUDED.input_schema_json,
		    output_schema_json = EXCLUDED.output_schema_json,
		    side_effect = EXCLUDED.side_effect,
		    requires_confirmation = EXCLUDED.requires_confirmation,
		    status = EXCLUDED.status,
		    last_synced_at = now(),
		    updated_at = now(),
		    deleted_at = NULL
		RETURNING `+mcpToolSelectColumns,
		normalizeText(params.ServerID),
		normalizeText(params.Name),
		normalizeText(params.NamespacedName),
		normalizeText(params.Description),
		jsonOrDefault(params.InputSchema),
		jsonOrDefault(params.OutputSchema),
		params.SideEffect,
		params.RequiresConfirmation,
		normalizeText(params.Status),
	))
	return tool, translateWriteError(err)
}

func (r *MCPRepository) ListActiveServers(ctx context.Context) ([]MCPServer, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+mcpServerSelectColumns+`
		FROM mcp_servers
		WHERE deleted_at IS NULL
		  AND status = 'active'
		ORDER BY name`)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	servers := []MCPServer{}
	for rows.Next() {
		server, err := scanMCPServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	if err := rows.Err(); err != nil {
		return nil, translateReadError(err)
	}
	return servers, nil
}

func (r *MCPRepository) ListActiveToolsForServer(ctx context.Context, serverID string) ([]MCPTool, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+mcpToolSelectColumns+`
		FROM mcp_tools
		WHERE server_id = $1::uuid
		  AND deleted_at IS NULL
		  AND status = 'active'
		ORDER BY name`,
		normalizeText(serverID),
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	tools := []MCPTool{}
	for rows.Next() {
		tool, err := scanMCPTool(rows)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	if err := rows.Err(); err != nil {
		return nil, translateReadError(err)
	}
	return tools, nil
}

func scanMCPServer(row scanner) (MCPServer, error) {
	var (
		id        pgtype.UUID
		deletedAt pgtype.Timestamptz
		server    MCPServer
	)
	err := row.Scan(
		&id,
		&server.Name,
		&server.DisplayName,
		&server.Transport,
		&server.Endpoint,
		&server.Command,
		&server.Args,
		&server.Env,
		&server.SecretRefs,
		&server.Status,
		&server.CreatedAt,
		&server.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return MCPServer{}, translateReadError(err)
	}
	server.ID = uuidString(id)
	server.Args = jsonOrDefaultArray(server.Args)
	server.Env = jsonOrDefault(server.Env)
	server.SecretRefs = jsonOrDefault(server.SecretRefs)
	if deletedAt.Valid {
		server.DeletedAt = &deletedAt.Time
	}
	return server, nil
}

func scanMCPTool(row scanner) (MCPTool, error) {
	var (
		id           pgtype.UUID
		serverID     pgtype.UUID
		lastSyncedAt pgtype.Timestamptz
		deletedAt    pgtype.Timestamptz
		tool         MCPTool
	)
	err := row.Scan(
		&id,
		&serverID,
		&tool.Name,
		&tool.NamespacedName,
		&tool.Description,
		&tool.InputSchema,
		&tool.OutputSchema,
		&tool.SideEffect,
		&tool.RequiresConfirmation,
		&tool.Status,
		&lastSyncedAt,
		&tool.CreatedAt,
		&tool.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return MCPTool{}, translateReadError(err)
	}
	tool.ID = uuidString(id)
	tool.ServerID = uuidString(serverID)
	tool.InputSchema = jsonOrDefault(tool.InputSchema)
	tool.OutputSchema = jsonOrDefault(tool.OutputSchema)
	if lastSyncedAt.Valid {
		tool.LastSyncedAt = &lastSyncedAt.Time
	}
	if deletedAt.Valid {
		tool.DeletedAt = &deletedAt.Time
	}
	return tool, nil
}
