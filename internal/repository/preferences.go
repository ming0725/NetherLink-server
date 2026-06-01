package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const userPreferenceSelectColumns = `
	user_id,
	theme_color,
	font_mode,
	input_effects,
	settings_json,
	version,
	created_at,
	updated_at`

type UserPreferenceRepository struct {
	db DBTX
}

type UserPreference struct {
	UserID       string
	ThemeColor   *string
	FontMode     *string
	InputEffects json.RawMessage
	Settings     json.RawMessage
	Version      int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UpsertUserPreferenceParams struct {
	UserID       string
	ThemeColor   *string
	FontMode     *string
	InputEffects json.RawMessage
	Settings     json.RawMessage
}

type UpdateUserPreferenceParams struct {
	UserID       string
	ThemeColor   *string
	FontMode     *string
	InputEffects json.RawMessage
	Settings     json.RawMessage
}

func NewUserPreferenceRepository(db DBTX) *UserPreferenceRepository {
	return &UserPreferenceRepository{db: db}
}

func (r *UserPreferenceRepository) EnsureDefault(ctx context.Context, userID string) (UserPreference, error) {
	row := r.db.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO user_preferences (user_id)
			VALUES ($1::uuid)
			ON CONFLICT (user_id) DO NOTHING
			RETURNING `+userPreferenceSelectColumns+`
		)
		SELECT `+userPreferenceSelectColumns+`
		FROM inserted
		UNION ALL
		SELECT `+userPreferenceSelectColumns+`
		FROM user_preferences
		WHERE user_id = $1::uuid
		LIMIT 1`,
		normalizeText(userID),
	)

	preferences, err := scanUserPreference(row)
	return preferences, translateWriteError(err)
}

func (r *UserPreferenceRepository) Upsert(ctx context.Context, params UpsertUserPreferenceParams) (UserPreference, error) {
	row := r.db.QueryRow(ctx, `
		INSERT INTO user_preferences (
			user_id,
			theme_color,
			font_mode,
			input_effects,
			settings_json
		)
		VALUES ($1::uuid, $2, $3, $4::jsonb, $5::jsonb)
		ON CONFLICT (user_id) DO UPDATE
		SET
			theme_color = EXCLUDED.theme_color,
			font_mode = EXCLUDED.font_mode,
			input_effects = EXCLUDED.input_effects,
			settings_json = EXCLUDED.settings_json,
			version = user_preferences.version + 1
		RETURNING `+userPreferenceSelectColumns,
		normalizeText(params.UserID),
		normalizeTextPtr(params.ThemeColor),
		normalizeTextPtr(params.FontMode),
		jsonOrDefault(params.InputEffects),
		jsonOrDefault(params.Settings),
	)

	preferences, err := scanUserPreference(row)
	return preferences, translateWriteError(err)
}

func (r *UserPreferenceRepository) GetByUserID(ctx context.Context, userID string) (UserPreference, error) {
	row := r.db.QueryRow(ctx, `
		SELECT `+userPreferenceSelectColumns+`
		FROM user_preferences
		WHERE user_id = $1::uuid`,
		normalizeText(userID),
	)

	preferences, err := scanUserPreference(row)
	return preferences, translateReadError(err)
}

func (r *UserPreferenceRepository) Update(ctx context.Context, params UpdateUserPreferenceParams) (UserPreference, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE user_preferences
		SET
			theme_color = COALESCE($2, theme_color),
			font_mode = COALESCE($3, font_mode),
			input_effects = COALESCE($4::jsonb, input_effects),
			settings_json = COALESCE($5::jsonb, settings_json),
			version = version + 1
		WHERE user_id = $1::uuid
		RETURNING `+userPreferenceSelectColumns,
		normalizeText(params.UserID),
		normalizeTextPtr(params.ThemeColor),
		normalizeTextPtr(params.FontMode),
		nullableJSON(params.InputEffects),
		nullableJSON(params.Settings),
	)

	preferences, err := scanUserPreference(row)
	return preferences, translateWriteError(err)
}

func scanUserPreference(row scanner) (UserPreference, error) {
	var (
		preferences UserPreference
		userID      pgtype.UUID
		themeColor  sql.NullString
		fontMode    sql.NullString
	)

	err := row.Scan(
		&userID,
		&themeColor,
		&fontMode,
		&preferences.InputEffects,
		&preferences.Settings,
		&preferences.Version,
		&preferences.CreatedAt,
		&preferences.UpdatedAt,
	)
	if err != nil {
		return UserPreference{}, err
	}

	preferences.UserID = uuidString(userID)
	if themeColor.Valid {
		preferences.ThemeColor = &themeColor.String
	}
	if fontMode.Valid {
		preferences.FontMode = &fontMode.String
	}
	preferences.InputEffects = jsonOrDefault(preferences.InputEffects)
	preferences.Settings = jsonOrDefault(preferences.Settings)

	return preferences, nil
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
