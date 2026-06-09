package server

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"nlserver/internal/realtime"
	"nlserver/internal/repository"
)

func TestWebSocketPresenceConnectRestoresPersistedStatus(t *testing.T) {
	db := &presenceStatusDB{status: "mining"}
	userID := "00000000-0000-0000-0000-000000000001"
	hub := realtime.NewHub()
	srv := newPresenceTestServer(db, hub)

	srv.handleWebSocketPresenceConnect(context.Background(), userID)

	state, ok := hub.Presence(userID)
	if !ok || state.Status != "mining" {
		t.Fatalf("presence = %+v ok=%v, want restored mining", state, ok)
	}
	if !db.executedSQLContains("last_seen_at") {
		t.Fatalf("connect did not touch last_seen_at; execs = %v", db.execs)
	}
}

func TestWebSocketPresenceDisconnectDoesNotOverwritePersistedStatus(t *testing.T) {
	db := &presenceStatusDB{status: "mining"}
	userID := "00000000-0000-0000-0000-000000000001"
	hub := realtime.NewHub()
	hub.SetPresence(userID, "mining", time.Now().UTC())
	srv := newPresenceTestServer(db, hub)

	srv.handleWebSocketPresenceDisconnect(context.Background(), userID)

	state, ok := hub.Presence(userID)
	if !ok || state.Status != "offline" {
		t.Fatalf("presence = %+v ok=%v, want runtime offline after disconnect", state, ok)
	}
	if db.executedSQLContains("presence_status") {
		t.Fatalf("disconnect overwrote persisted presence status; execs = %v", db.execs)
	}
}

func newPresenceTestServer(db *presenceStatusDB, hub *realtime.Hub) *Server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewWithDependencies(Dependencies{
		Repos: repository.Repositories{
			Users: repository.NewUserRepository(db),
		},
		RealtimeHub: hub,
		Logger:      logger,
	})
}

type presenceStatusDB struct {
	status string
	execs  []string
}

func (db *presenceStatusDB) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	db.execs = append(db.execs, sql)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (db *presenceStatusDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, fmt.Errorf("unexpected query")
}

func (db *presenceStatusDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return presenceUserRow{status: db.status}
}

func (db *presenceStatusDB) executedSQLContains(value string) bool {
	for _, query := range db.execs {
		if strings.Contains(query, value) {
			return true
		}
	}
	return false
}

type presenceUserRow struct {
	status string
}

func (row presenceUserRow) Scan(dest ...any) error {
	if len(dest) != 18 {
		return fmt.Errorf("scan dest count = %d, want 18", len(dest))
	}
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	userID := pgtype.UUID{Bytes: [16]byte{15: 1}, Valid: true}
	values := []any{
		userID,
		"restore-user",
		"restore@example.com",
		"hash",
		"Restore User",
		"unspecified",
		sql.NullString{},
		pgtype.UUID{},
		"active",
		sql.NullString{},
		sql.NullString{},
		sql.NullTime{},
		row.status,
		1,
		1,
		now,
		now,
		sql.NullTime{},
	}
	for i, target := range dest {
		if err := assignPresenceScanValue(target, values[i]); err != nil {
			return fmt.Errorf("scan dest %d: %w", i, err)
		}
	}
	return nil
}

func assignPresenceScanValue(target any, value any) error {
	switch dest := target.(type) {
	case *pgtype.UUID:
		*dest = value.(pgtype.UUID)
	case *string:
		*dest = value.(string)
	case *sql.NullString:
		*dest = value.(sql.NullString)
	case *sql.NullTime:
		*dest = value.(sql.NullTime)
	case *int:
		*dest = value.(int)
	case *time.Time:
		*dest = value.(time.Time)
	default:
		return fmt.Errorf("unsupported destination %T", target)
	}
	return nil
}
