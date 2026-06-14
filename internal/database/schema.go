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
	} {
		if _, err := db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
