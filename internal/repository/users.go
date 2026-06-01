package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const userSelectColumns = `
	id,
	public_id,
	email,
	password_hash,
	nick,
	gender,
	gender_custom,
	avatar_file_id,
	status,
	signature,
	region,
	last_seen_at,
	token_version,
	version,
	created_at,
	updated_at,
	deleted_at`

type UserRepository struct {
	db DBTX
}

type User struct {
	ID           string
	PublicID     string
	Email        string
	PasswordHash string
	Nick         string
	Gender       string
	GenderCustom *string
	AvatarFileID *string
	Status       string
	Signature    *string
	Region       *string
	LastSeenAt   *time.Time
	TokenVersion int
	Version      int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

type CreateUserParams struct {
	PublicID     string
	Email        string
	PasswordHash string
	Nick         string
	Gender       string
	GenderCustom *string
	AvatarFileID *string
	Status       string
	Signature    *string
	Region       *string
}

type UpdateUserProfileParams struct {
	UserID       string
	Nick         *string
	Gender       *string
	GenderCustom *string
	Status       *string
	Signature    *string
	Region       *string
}

func NewUserRepository(db DBTX) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, params CreateUserParams) (User, error) {
	gender := normalizeText(params.Gender)
	if gender == "" {
		gender = "unspecified"
	}

	status := normalizeText(params.Status)
	if status == "" {
		status = "active"
	}

	row := r.db.QueryRow(ctx, `
		INSERT INTO users (
			public_id,
			email,
			password_hash,
			nick,
			gender,
			gender_custom,
			avatar_file_id,
			status,
			signature,
			region
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::uuid, $8, $9, $10)
		RETURNING `+userSelectColumns,
		normalizeText(params.PublicID),
		normalizeEmail(params.Email),
		params.PasswordHash,
		normalizeText(params.Nick),
		gender,
		normalizeTextPtr(params.GenderCustom),
		normalizeTextPtr(params.AvatarFileID),
		status,
		normalizeTextPtr(params.Signature),
		normalizeTextPtr(params.Region),
	)

	user, err := scanUser(row)
	return user, translateWriteError(err)
}

func (r *UserRepository) GetByID(ctx context.Context, userID string) (User, error) {
	return r.getOne(ctx, `SELECT `+userSelectColumns+` FROM users WHERE id = $1::uuid AND deleted_at IS NULL`, normalizeText(userID))
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (User, error) {
	return r.getOne(ctx, `SELECT `+userSelectColumns+` FROM users WHERE email = $1 AND deleted_at IS NULL`, normalizeEmail(email))
}

func (r *UserRepository) GetByPublicID(ctx context.Context, publicID string) (User, error) {
	return r.getOne(ctx, `SELECT `+userSelectColumns+` FROM users WHERE public_id = $1 AND deleted_at IS NULL`, normalizeText(publicID))
}

func (r *UserRepository) GetByAccount(ctx context.Context, account string) (User, error) {
	normalized := normalizeText(account)
	if containsAtSign(normalized) {
		return r.GetByEmail(ctx, normalized)
	}
	return r.GetByPublicID(ctx, normalized)
}

func (r *UserRepository) PublicIDExists(ctx context.Context, publicID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users
			WHERE public_id = $1
			  AND deleted_at IS NULL
		)`,
		normalizeText(publicID),
	).Scan(&exists)
	return exists, translateReadError(err)
}

func (r *UserRepository) UpdateProfile(ctx context.Context, params UpdateUserProfileParams) (User, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE users
		SET
			nick = COALESCE($2, nick),
			gender = COALESCE($3, gender),
			gender_custom = COALESCE($4, gender_custom),
			status = COALESCE($5, status),
			signature = COALESCE($6, signature),
			region = COALESCE($7, region),
			version = version + 1
		WHERE id = $1::uuid
		  AND deleted_at IS NULL
		RETURNING `+userSelectColumns,
		normalizeText(params.UserID),
		normalizeTextPtr(params.Nick),
		normalizeTextPtr(params.Gender),
		normalizeTextPtr(params.GenderCustom),
		normalizeTextPtr(params.Status),
		normalizeTextPtr(params.Signature),
		normalizeTextPtr(params.Region),
	)

	user, err := scanUser(row)
	return user, translateWriteError(err)
}

func (r *UserRepository) UpdatePublicID(ctx context.Context, userID string, publicID string, clientOperationID *string) (User, error) {
	row := r.db.QueryRow(ctx, `
		WITH existing AS (
			SELECT public_id
			FROM users
			WHERE id = $1::uuid
			  AND deleted_at IS NULL
		), updated AS (
			UPDATE users
			SET public_id = $2,
			    version = version + 1
			WHERE id = $1::uuid
			  AND deleted_at IS NULL
			RETURNING `+userSelectColumns+`
		), inserted_history AS (
			INSERT INTO user_id_changes (
				user_id,
				old_public_id,
				new_public_id,
				client_operation_id
			)
			SELECT $1::uuid, existing.public_id, $2, $3
			FROM existing
			ON CONFLICT (user_id, client_operation_id) DO NOTHING
			RETURNING id
		)
		SELECT `+userSelectColumns+`
		FROM updated`,
		normalizeText(userID),
		normalizeText(publicID),
		normalizeTextPtr(clientOperationID),
	)

	user, err := scanUser(row)
	return user, translateWriteError(err)
}

func (r *UserRepository) IncrementTokenVersion(ctx context.Context, userID string) (User, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE users
		SET token_version = token_version + 1,
		    version = version + 1
		WHERE id = $1::uuid
		  AND deleted_at IS NULL
		RETURNING `+userSelectColumns,
		normalizeText(userID),
	)

	user, err := scanUser(row)
	return user, translateWriteError(err)
}

func (r *UserRepository) TouchLastSeen(ctx context.Context, userID string, seenAt time.Time) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE users
		SET last_seen_at = $2
		WHERE id = $1::uuid
		  AND deleted_at IS NULL`,
		normalizeText(userID),
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

func (r *UserRepository) getOne(ctx context.Context, query string, args ...any) (User, error) {
	user, err := scanUser(r.db.QueryRow(ctx, query, args...))
	return user, translateReadError(err)
}

func scanUser(row scanner) (User, error) {
	var (
		user         User
		id           pgtype.UUID
		genderCustom sql.NullString
		avatarFileID pgtype.UUID
		signature    sql.NullString
		region       sql.NullString
		lastSeenAt   sql.NullTime
		deletedAt    sql.NullTime
	)

	err := row.Scan(
		&id,
		&user.PublicID,
		&user.Email,
		&user.PasswordHash,
		&user.Nick,
		&user.Gender,
		&genderCustom,
		&avatarFileID,
		&user.Status,
		&signature,
		&region,
		&lastSeenAt,
		&user.TokenVersion,
		&user.Version,
		&user.CreatedAt,
		&user.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return User{}, err
	}

	user.ID = uuidString(id)
	if genderCustom.Valid {
		user.GenderCustom = &genderCustom.String
	}
	user.AvatarFileID = uuidStringPtr(avatarFileID)
	if signature.Valid {
		user.Signature = &signature.String
	}
	if region.Valid {
		user.Region = &region.String
	}
	if lastSeenAt.Valid {
		user.LastSeenAt = &lastSeenAt.Time
	}
	if deletedAt.Valid {
		user.DeletedAt = &deletedAt.Time
	}

	return user, nil
}

func containsAtSign(value string) bool {
	for _, char := range value {
		if char == '@' {
			return true
		}
	}
	return false
}
