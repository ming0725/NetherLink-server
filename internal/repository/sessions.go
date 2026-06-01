package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const userSessionSelectColumns = `
	id,
	user_id,
	refresh_token_hash,
	device_id,
	device_name,
	expires_at,
	revoked_at,
	last_seen_at,
	created_at,
	updated_at`

type UserSessionRepository struct {
	db DBTX
}

type UserSession struct {
	ID               string
	UserID           string
	RefreshTokenHash string
	DeviceID         string
	DeviceName       *string
	ExpiresAt        time.Time
	RevokedAt        *time.Time
	LastSeenAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateUserSessionParams struct {
	UserID           string
	RefreshTokenHash string
	DeviceID         string
	DeviceName       *string
	ExpiresAt        time.Time
	LastSeenAt       *time.Time
}

func NewUserSessionRepository(db DBTX) *UserSessionRepository {
	return &UserSessionRepository{db: db}
}

func (r *UserSessionRepository) Create(ctx context.Context, params CreateUserSessionParams) (UserSession, error) {
	row := r.db.QueryRow(ctx, `
		INSERT INTO user_sessions (
			user_id,
			refresh_token_hash,
			device_id,
			device_name,
			expires_at,
			last_seen_at
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6)
		RETURNING `+userSessionSelectColumns,
		normalizeText(params.UserID),
		params.RefreshTokenHash,
		normalizeText(params.DeviceID),
		normalizeTextPtr(params.DeviceName),
		params.ExpiresAt,
		params.LastSeenAt,
	)

	session, err := scanUserSession(row)
	return session, translateWriteError(err)
}

func (r *UserSessionRepository) GetByID(ctx context.Context, sessionID string) (UserSession, error) {
	return r.getOne(ctx, `
		SELECT `+userSessionSelectColumns+`
		FROM user_sessions
		WHERE id = $1::uuid`,
		normalizeText(sessionID),
	)
}

func (r *UserSessionRepository) GetActiveByID(ctx context.Context, sessionID string) (UserSession, error) {
	return r.getOne(ctx, `
		SELECT `+userSessionSelectColumns+`
		FROM user_sessions
		WHERE id = $1::uuid
		  AND revoked_at IS NULL
		  AND expires_at > now()`,
		normalizeText(sessionID),
	)
}

func (r *UserSessionRepository) GetActiveByRefreshTokenHash(ctx context.Context, refreshTokenHash string) (UserSession, error) {
	return r.getOne(ctx, `
		SELECT `+userSessionSelectColumns+`
		FROM user_sessions
		WHERE refresh_token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > now()`,
		refreshTokenHash,
	)
}

func (r *UserSessionRepository) ListByUserID(ctx context.Context, userID string, activeOnly bool) ([]UserSession, error) {
	query := `
		SELECT ` + userSessionSelectColumns + `
		FROM user_sessions
		WHERE user_id = $1::uuid`
	if activeOnly {
		query += `
		  AND revoked_at IS NULL
		  AND expires_at > now()`
	}
	query += `
		ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, query, normalizeText(userID))
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	sessions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (UserSession, error) {
		return scanUserSession(row)
	})
	if err != nil {
		return nil, translateReadError(err)
	}

	return sessions, nil
}

func (r *UserSessionRepository) RotateRefreshToken(ctx context.Context, sessionID string, oldRefreshTokenHash string, newRefreshTokenHash string, expiresAt time.Time) (UserSession, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE user_sessions
		SET
			refresh_token_hash = $3,
			expires_at = $4,
			last_seen_at = now()
		WHERE id = $1::uuid
		  AND refresh_token_hash = $2
		  AND revoked_at IS NULL
		  AND expires_at > now()
		RETURNING `+userSessionSelectColumns,
		normalizeText(sessionID),
		oldRefreshTokenHash,
		newRefreshTokenHash,
		expiresAt,
	)

	session, err := scanUserSession(row)
	return session, translateWriteError(err)
}

func (r *UserSessionRepository) Revoke(ctx context.Context, sessionID string) error {
	return r.revokeWhere(ctx, `id = $1::uuid`, normalizeText(sessionID))
}

func (r *UserSessionRepository) RevokeByRefreshTokenHash(ctx context.Context, refreshTokenHash string) error {
	return r.revokeWhere(ctx, `refresh_token_hash = $1`, refreshTokenHash)
}

func (r *UserSessionRepository) RevokeByUserID(ctx context.Context, userID string) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE user_sessions
		SET revoked_at = now()
		WHERE user_id = $1::uuid
		  AND revoked_at IS NULL`,
		normalizeText(userID),
	)
	if err != nil {
		return 0, translateWriteError(err)
	}
	return tag.RowsAffected(), nil
}

func (r *UserSessionRepository) TouchLastSeen(ctx context.Context, sessionID string, seenAt time.Time) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE user_sessions
		SET last_seen_at = $2
		WHERE id = $1::uuid
		  AND revoked_at IS NULL
		  AND expires_at > now()`,
		normalizeText(sessionID),
		seenAt,
	)
	if err != nil {
		return translateWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *UserSessionRepository) getOne(ctx context.Context, query string, args ...any) (UserSession, error) {
	session, err := scanUserSession(r.db.QueryRow(ctx, query, args...))
	return session, translateReadError(err)
}

func (r *UserSessionRepository) revokeWhere(ctx context.Context, predicate string, args ...any) error {
	query := `
		UPDATE user_sessions
		SET revoked_at = now()
		WHERE ` + predicate + `
		  AND revoked_at IS NULL`

	tag, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		return translateWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanUserSession(row scanner) (UserSession, error) {
	var (
		session    UserSession
		id         pgtype.UUID
		userID     pgtype.UUID
		deviceName sql.NullString
		revokedAt  sql.NullTime
		lastSeenAt sql.NullTime
	)

	err := row.Scan(
		&id,
		&userID,
		&session.RefreshTokenHash,
		&session.DeviceID,
		&deviceName,
		&session.ExpiresAt,
		&revokedAt,
		&lastSeenAt,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err != nil {
		return UserSession{}, err
	}

	session.ID = uuidString(id)
	session.UserID = uuidString(userID)
	if deviceName.Valid {
		session.DeviceName = &deviceName.String
	}
	if revokedAt.Valid {
		session.RevokedAt = &revokedAt.Time
	}
	if lastSeenAt.Valid {
		session.LastSeenAt = &lastSeenAt.Time
	}

	return session, nil
}
