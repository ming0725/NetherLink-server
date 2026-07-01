package database

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestEnsureRuntimeSchemaAddsCompatibilityColumns(t *testing.T) {
	db := &schemaTestDB{}

	if err := EnsureRuntimeSchema(context.Background(), db); err != nil {
		t.Fatalf("EnsureRuntimeSchema error = %v", err)
	}

	for _, want := range []string{
		"ALTER TABLE users ADD COLUMN IF NOT EXISTS presence_status",
		"ALTER TABLE groups ADD COLUMN IF NOT EXISTS avatar_version",
		"ALTER TABLE groups ADD COLUMN IF NOT EXISTS public_id",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_public_id_unique",
		"CREATE TABLE IF NOT EXISTS conversation_device_states",
		"CREATE INDEX IF NOT EXISTS idx_conversation_device_states_user_device",
		"CREATE TABLE IF NOT EXISTS message_mentions",
		"ALTER TABLE message_mentions DROP CONSTRAINT IF EXISTS message_mentions_target_type_check",
		"ALTER TABLE message_mentions ADD CONSTRAINT message_mentions_target_type_check",
		"CREATE INDEX IF NOT EXISTS idx_message_mentions_user_id",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_message_mentions_message_user_unique",
		"CREATE TABLE IF NOT EXISTS group_states",
		"CREATE INDEX IF NOT EXISTS idx_group_states_group_type",
		"CREATE TABLE IF NOT EXISTS group_chat_summaries",
		"CREATE INDEX IF NOT EXISTS idx_group_chat_summaries_group_period",
		"SET role = 'ai'",
		"CREATE TABLE IF NOT EXISTS ai_message_traces",
		"CREATE INDEX IF NOT EXISTS idx_ai_message_traces_conversation_created",
		"trg_conversation_device_states_set_updated_at",
	} {
		if !db.executed(want) {
			t.Fatalf("missing schema statement containing %q; statements = %v", want, db.statements)
		}
	}
}

type schemaTestDB struct {
	statements []string
}

func (db *schemaTestDB) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	db.statements = append(db.statements, sql)
	return pgconn.NewCommandTag("ALTER TABLE"), nil
}

func (db *schemaTestDB) executed(value string) bool {
	for _, statement := range db.statements {
		if strings.Contains(statement, value) {
			return true
		}
	}
	return false
}
