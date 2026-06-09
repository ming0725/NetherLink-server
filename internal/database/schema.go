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
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS avatar_version INTEGER NOT NULL DEFAULT 1`,
	} {
		if _, err := db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
