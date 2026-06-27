package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

type schemaExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func EnsureRuntimeSchema(ctx context.Context, db schemaExecer) error {
	if db == nil {
		return nil
	}
	for _, statement := range []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS presence_status TEXT NOT NULL DEFAULT 'online'`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS account_type TEXT NOT NULL DEFAULT 'human'`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS avatar_version INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS public_id TEXT`,
		`WITH numbered AS (
			SELECT id, (1000000000 + row_number() OVER (ORDER BY created_at, id))::text AS generated_public_id
			FROM groups
			WHERE public_id IS NULL
		)
		UPDATE groups
		SET public_id = numbered.generated_public_id
		FROM numbered
		WHERE groups.id = numbered.id`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_public_id_unique ON groups(public_id)`,
		`ALTER TABLE groups ALTER COLUMN public_id SET NOT NULL`,
		`CREATE TABLE IF NOT EXISTS conversation_device_states (
			conversation_id UUID NOT NULL,
			user_id UUID NOT NULL,
			device_id TEXT NOT NULL,
			hidden_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (conversation_id, user_id, device_id),
			FOREIGN KEY (conversation_id, user_id) REFERENCES conversation_participants(conversation_id, user_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_device_states_user_device ON conversation_device_states(user_id, device_id)`,
		`CREATE TABLE IF NOT EXISTS ai_message_traces (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			conversation_id UUID NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
			message_id UUID NOT NULL UNIQUE REFERENCES ai_messages(id) ON DELETE CASCADE,
			status TEXT NOT NULL DEFAULT 'completed',
			summary TEXT NOT NULL DEFAULT '',
			steps_json JSONB NOT NULL DEFAULT '[]'::jsonb,
			source_refs_json JSONB NOT NULL DEFAULT '[]'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_message_traces_conversation_created ON ai_message_traces(conversation_id, created_at ASC)`,
		`CREATE TABLE IF NOT EXISTS agent_style_profiles (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			source_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			consent_status TEXT NOT NULL DEFAULT 'pending',
			sample_refs_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			style_summary TEXT NOT NULL DEFAULT '',
			style_embedding DOUBLE PRECISION[],
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS agent_profiles (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			bot_user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
			owner_user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			visibility TEXT NOT NULL DEFAULT 'private',
			name TEXT NOT NULL,
			avatar_file_id UUID REFERENCES files(id) ON DELETE SET NULL,
			base_prompt TEXT NOT NULL DEFAULT '',
			model TEXT,
			tool_policy_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			safety_policy_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			style_profile_id UUID REFERENCES agent_style_profiles(id) ON DELETE SET NULL,
			status TEXT NOT NULL DEFAULT 'active',
			version INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS agent_bindings (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			agent_id UUID NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
			scope_type TEXT NOT NULL,
			scope_id UUID NOT NULL,
			trigger_mode TEXT NOT NULL DEFAULT 'manual',
			enabled BOOLEAN NOT NULL DEFAULT true,
			cooldown_seconds INTEGER NOT NULL DEFAULT 0,
			created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ,
			UNIQUE (agent_id, scope_type, scope_id)
		)`,
		`CREATE TABLE IF NOT EXISTS agent_runs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			agent_id UUID NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
			invoker_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
			source_type TEXT NOT NULL,
			source_id UUID,
			status TEXT NOT NULL DEFAULT 'queued',
			input_snapshot_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			context_refs_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			tool_calls_json JSONB NOT NULL DEFAULT '[]'::jsonb,
			output_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			model TEXT,
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0,
			error_code TEXT,
			client_operation_id TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS agent_memories (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			agent_id UUID NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			scope_type TEXT NOT NULL,
			scope_id UUID,
			memory_type TEXT NOT NULL,
			content TEXT NOT NULL,
			source_refs_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			confidence DOUBLE PRECISION NOT NULL DEFAULT 1,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS rag_documents (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			source_type TEXT NOT NULL,
			source_id UUID NOT NULL,
			owner_user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			visibility TEXT NOT NULL,
			title TEXT,
			metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			version INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ,
			UNIQUE (source_type, source_id)
		)`,
		`CREATE TABLE IF NOT EXISTS rag_chunks (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			document_id UUID NOT NULL REFERENCES rag_documents(id) ON DELETE CASCADE,
			chunk_index INTEGER NOT NULL,
			content TEXT NOT NULL,
			token_count INTEGER NOT NULL DEFAULT 0,
			embedding_vector DOUBLE PRECISION[],
			metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (document_id, chunk_index)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_profiles_owner_updated ON agent_profiles(owner_user_id, updated_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_agent_profiles_kind_status ON agent_profiles(kind, status) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_agent_bindings_scope ON agent_bindings(scope_type, scope_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_agent_runs_agent_created ON agent_runs(agent_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_runs_invoker_created ON agent_runs(invoker_user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_memories_scope ON agent_memories(agent_id, user_id, scope_type, scope_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_rag_documents_source ON rag_documents(source_type, source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_rag_chunks_document_index ON rag_chunks(document_id, chunk_index)`,
		`CREATE TABLE IF NOT EXISTS mcp_servers (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL DEFAULT '',
			transport TEXT NOT NULL,
			endpoint TEXT,
			command TEXT,
			args_json JSONB NOT NULL DEFAULT '[]'::jsonb,
			env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			secret_refs_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			status TEXT NOT NULL DEFAULT 'disabled',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_tools (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			server_id UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			namespaced_name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			input_schema_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			output_schema_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			side_effect BOOLEAN NOT NULL DEFAULT false,
			requires_confirmation BOOLEAN NOT NULL DEFAULT true,
			status TEXT NOT NULL DEFAULT 'active',
			last_synced_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ,
			UNIQUE (server_id, name),
			UNIQUE (namespaced_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_servers_status_name ON mcp_servers(status, name) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_tools_server_status ON mcp_tools(server_id, status) WHERE deleted_at IS NULL`,
		`DO $$
		BEGIN
			IF to_regproc('set_updated_at') IS NOT NULL
			   AND NOT EXISTS (
				SELECT 1
				FROM pg_trigger
				WHERE tgname = 'trg_conversation_device_states_set_updated_at'
			   ) THEN
				CREATE TRIGGER trg_conversation_device_states_set_updated_at
				BEFORE UPDATE ON conversation_device_states
				FOR EACH ROW EXECUTE FUNCTION set_updated_at();
			END IF;
		END $$`,
		`DO $$
		DECLARE
			table_name TEXT;
		BEGIN
			IF to_regproc('set_updated_at') IS NULL THEN
				RETURN;
			END IF;
			FOREACH table_name IN ARRAY ARRAY[
				'ai_message_traces',
				'agent_style_profiles',
				'agent_profiles',
				'agent_bindings',
				'agent_runs',
				'agent_memories',
				'rag_documents',
				'rag_chunks',
				'mcp_servers',
				'mcp_tools'
			]
			LOOP
				IF NOT EXISTS (
					SELECT 1
					FROM pg_trigger
					WHERE tgname = format('trg_%s_set_updated_at', table_name)
				) THEN
					EXECUTE format(
						'CREATE TRIGGER trg_%I_set_updated_at BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION set_updated_at()',
						table_name,
						table_name
					);
				END IF;
			END LOOP;
		END $$`,
	} {
		if _, err := db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
