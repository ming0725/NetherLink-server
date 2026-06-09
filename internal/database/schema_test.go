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
