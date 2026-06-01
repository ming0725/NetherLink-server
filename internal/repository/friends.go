package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type FriendRepository struct {
	db DBTX
}

type FriendGroup struct {
	ID        string
	UserID    string
	Name      string
	SortOrder int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Friend struct {
	ID            string
	User          User
	Remark        *string
	FriendGroupID *string
	IsDND         bool
	Status        string
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewFriendRepository(db DBTX) *FriendRepository {
	return &FriendRepository{db: db}
}

func (r *FriendRepository) ListGroups(ctx context.Context, userID string, keyword string) ([]FriendGroup, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			id,
			user_id,
			name,
			sort_order,
			created_at,
			updated_at
		FROM friend_groups
		WHERE user_id = $1::uuid
		  AND deleted_at IS NULL
		  AND ($2 = '' OR name ILIKE '%' || $2 || '%')
		ORDER BY sort_order ASC, created_at ASC`,
		normalizeText(userID),
		normalizeText(keyword),
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	groups, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (FriendGroup, error) {
		return scanFriendGroup(row)
	})
	if err != nil {
		return nil, translateReadError(err)
	}

	return groups, nil
}

func (r *FriendRepository) ListFriends(ctx context.Context, userID string, keyword string, groupID string, limit int, offset int) ([]Friend, int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			f.id,
			f.remark,
			f.friend_group_id,
			f.is_dnd,
			f.status,
			f.version,
			f.created_at,
			f.updated_at,
			u.id,
			u.public_id,
			u.email,
			u.password_hash,
			u.nick,
			u.gender,
			u.gender_custom,
			u.avatar_file_id,
			u.status,
			u.signature,
			u.region,
			u.last_seen_at,
			u.token_version,
			u.version,
			u.created_at,
			u.updated_at,
			u.deleted_at,
			(count(*) OVER())::int AS total_count
		FROM friendships f
		JOIN users u ON u.id = f.friend_user_id
		WHERE f.user_id = $1::uuid
		  AND f.status = 'active'
		  AND f.deleted_at IS NULL
		  AND u.deleted_at IS NULL
		  AND u.status = 'active'
		  AND ($2 = '' OR f.friend_group_id = NULLIF($2, '')::uuid)
		  AND (
		    $3 = ''
		    OR u.public_id ILIKE '%' || $3 || '%'
		    OR u.nick ILIKE '%' || $3 || '%'
		    OR f.remark ILIKE '%' || $3 || '%'
		  )
		ORDER BY COALESCE(f.remark, u.nick) ASC, u.public_id ASC
		LIMIT $4 OFFSET $5`,
		normalizeText(userID),
		normalizeText(groupID),
		normalizeText(keyword),
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, translateReadError(err)
	}
	defer rows.Close()

	var total int
	friends, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Friend, error) {
		friend, rowTotal, err := scanFriendWithTotal(row)
		if err != nil {
			return Friend{}, err
		}
		total = rowTotal
		return friend, nil
	})
	if err != nil {
		return nil, 0, translateReadError(err)
	}

	return friends, total, nil
}

func (r *FriendRepository) GetRelation(ctx context.Context, userID string, friendUserID string) (Friend, error) {
	friend, err := scanFriendWithNoTotal(r.db.QueryRow(ctx, `
		SELECT
			f.id,
			f.remark,
			f.friend_group_id,
			f.is_dnd,
			f.status,
			f.version,
			f.created_at,
			f.updated_at,
			u.id,
			u.public_id,
			u.email,
			u.password_hash,
			u.nick,
			u.gender,
			u.gender_custom,
			u.avatar_file_id,
			u.status,
			u.signature,
			u.region,
			u.last_seen_at,
			u.token_version,
			u.version,
			u.created_at,
			u.updated_at,
			u.deleted_at
		FROM friendships f
		JOIN users u ON u.id = f.friend_user_id
		WHERE f.user_id = $1::uuid
		  AND f.friend_user_id = $2::uuid
		  AND f.status = 'active'
		  AND f.deleted_at IS NULL
		  AND u.deleted_at IS NULL`,
		normalizeText(userID),
		normalizeText(friendUserID),
	))
	return friend, translateReadError(err)
}

func scanFriendGroup(row scanner) (FriendGroup, error) {
	var (
		group  FriendGroup
		id     pgtype.UUID
		userID pgtype.UUID
	)

	err := row.Scan(
		&id,
		&userID,
		&group.Name,
		&group.SortOrder,
		&group.CreatedAt,
		&group.UpdatedAt,
	)
	if err != nil {
		return FriendGroup{}, err
	}

	group.ID = uuidString(id)
	group.UserID = uuidString(userID)
	return group, nil
}

func scanFriendWithTotal(row scanner) (Friend, int, error) {
	friend, total, err := scanFriend(row, true)
	return friend, total, err
}

func scanFriendWithNoTotal(row scanner) (Friend, error) {
	friend, _, err := scanFriend(row, false)
	return friend, err
}

func scanFriend(row scanner, withTotal bool) (Friend, int, error) {
	var (
		friend       Friend
		friendID     pgtype.UUID
		remark       sql.NullString
		friendGroup  pgtype.UUID
		userID       pgtype.UUID
		genderCustom sql.NullString
		avatarFileID pgtype.UUID
		signature    sql.NullString
		region       sql.NullString
		lastSeenAt   sql.NullTime
		deletedAt    sql.NullTime
		total        int
	)

	dest := []any{
		&friendID,
		&remark,
		&friendGroup,
		&friend.IsDND,
		&friend.Status,
		&friend.Version,
		&friend.CreatedAt,
		&friend.UpdatedAt,
		&userID,
		&friend.User.PublicID,
		&friend.User.Email,
		&friend.User.PasswordHash,
		&friend.User.Nick,
		&friend.User.Gender,
		&genderCustom,
		&avatarFileID,
		&friend.User.Status,
		&signature,
		&region,
		&lastSeenAt,
		&friend.User.TokenVersion,
		&friend.User.Version,
		&friend.User.CreatedAt,
		&friend.User.UpdatedAt,
		&deletedAt,
	}
	if withTotal {
		dest = append(dest, &total)
	}

	if err := row.Scan(dest...); err != nil {
		return Friend{}, 0, err
	}

	friend.ID = uuidString(friendID)
	if remark.Valid {
		friend.Remark = &remark.String
	}
	friend.FriendGroupID = uuidStringPtr(friendGroup)
	friend.User.ID = uuidString(userID)
	if genderCustom.Valid {
		friend.User.GenderCustom = &genderCustom.String
	}
	friend.User.AvatarFileID = uuidStringPtr(avatarFileID)
	if signature.Valid {
		friend.User.Signature = &signature.String
	}
	if region.Valid {
		friend.User.Region = &region.String
	}
	if lastSeenAt.Valid {
		friend.User.LastSeenAt = &lastSeenAt.Time
	}
	if deletedAt.Valid {
		friend.User.DeletedAt = &deletedAt.Time
	}

	return friend, total, nil
}
