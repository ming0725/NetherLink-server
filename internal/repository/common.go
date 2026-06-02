package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrNotFound = errors.New("repository: not found")
	ErrConflict = errors.New("repository: conflict")
)

type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repositories struct {
	Users             *UserRepository
	Preferences       *UserPreferenceRepository
	Sessions          *UserSessionRepository
	Files             *FileRepository
	Chat              *ChatRepository
	Friends           *FriendRepository
	Groups            *GroupRepository
	Posts             *PostRepository
	RealtimeEvents    *RealtimeEventRepository
	OperationReceipts *OperationReceiptRepository
}

func New(db DBTX) Repositories {
	return Repositories{
		Users:             NewUserRepository(db),
		Preferences:       NewUserPreferenceRepository(db),
		Sessions:          NewUserSessionRepository(db),
		Files:             NewFileRepository(db),
		Chat:              NewChatRepository(db),
		Friends:           NewFriendRepository(db),
		Groups:            NewGroupRepository(db),
		Posts:             NewPostRepository(db),
		RealtimeEvents:    NewRealtimeEventRepository(db),
		OperationReceipts: NewOperationReceiptRepository(db),
	}
}

type scanner interface {
	Scan(dest ...any) error
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func normalizeText(value string) string {
	return strings.TrimSpace(value)
}

func normalizeTextPtr(value *string) *string {
	if value == nil {
		return nil
	}

	normalized := normalizeText(*value)
	return &normalized
}

func jsonOrDefault(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return value
}

func uuidString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return value.String()
}

func uuidStringPtr(value pgtype.UUID) *string {
	if !value.Valid {
		return nil
	}

	encoded := value.String()
	return &encoded
}

func translateReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func translateWriteError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
