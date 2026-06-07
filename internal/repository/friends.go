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
	ID              string
	User            User
	Remark          *string
	FriendGroupID   *string
	FriendGroupName *string
	IsDND           bool
	Status          string
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type FriendRequest struct {
	ID             string
	FromUser       User
	ToUser         User
	Message        *string
	SourceType     *string
	SourceGroupID  *string
	SourceFriendID *string
	Status         string
	HandledAt      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CreateFriendGroupParams struct {
	UserID    string
	Name      string
	SortOrder int
}

type UpdateFriendParams struct {
	UserID       string
	FriendUserID string
	RemarkSet    bool
	Remark       *string
	GroupIDSet   bool
	GroupID      *string
	IsDND        *bool
}

type CreateFriendRequestParams struct {
	FromUserID     string
	ToUserID       string
	Message        *string
	SourceType     *string
	SourceGroupID  *string
	SourceFriendID *string
}

type UpsertFriendshipParams struct {
	UserID        string
	FriendUserID  string
	Remark        *string
	FriendGroupID *string
	IsDND         bool
}

func NewFriendRepository(db DBTX) *FriendRepository {
	return &FriendRepository{db: db}
}

func (r *FriendRepository) CreateGroup(ctx context.Context, params CreateFriendGroupParams) (FriendGroup, error) {
	group, err := scanFriendGroup(r.db.QueryRow(ctx, `
		INSERT INTO friend_groups (
			user_id,
			name,
			sort_order
		)
		VALUES ($1::uuid, $2, $3)
		RETURNING
			id,
			user_id,
			name,
			sort_order,
			created_at,
			updated_at`,
		normalizeText(params.UserID),
		normalizeText(params.Name),
		params.SortOrder,
	))
	return group, translateWriteError(err)
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

func (r *FriendRepository) GroupExists(ctx context.Context, userID string, groupID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM friend_groups
			WHERE id = $2::uuid
			  AND user_id = $1::uuid
			  AND deleted_at IS NULL
		)`,
		normalizeText(userID),
		normalizeText(groupID),
	).Scan(&exists)
	return exists, translateReadError(err)
}

func (r *FriendRepository) ListFriends(ctx context.Context, userID string, keyword string, groupID string, limit int, offset int) ([]Friend, int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			f.id,
			f.remark,
			f.friend_group_id,
			fg.name,
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
		LEFT JOIN friend_groups fg ON fg.id = f.friend_group_id AND fg.deleted_at IS NULL
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

func (r *FriendRepository) ListActiveFriendUserIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT friend_user_id
		FROM friendships
		WHERE user_id = $1::uuid
		  AND status = 'active'
		ORDER BY friend_user_id`,
		normalizeText(userID),
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	userIDs := []string{}
	for rows.Next() {
		var friendUserID pgtype.UUID
		if err := rows.Scan(&friendUserID); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, uuidString(friendUserID))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return userIDs, nil
}

func (r *FriendRepository) GetRelation(ctx context.Context, userID string, friendUserID string) (Friend, error) {
	friend, err := scanFriendWithNoTotal(r.db.QueryRow(ctx, `
		SELECT
			f.id,
			f.remark,
			f.friend_group_id,
			fg.name,
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
		LEFT JOIN friend_groups fg ON fg.id = f.friend_group_id AND fg.deleted_at IS NULL
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

func (r *FriendRepository) UpdateFriend(ctx context.Context, params UpdateFriendParams) (Friend, error) {
	friend, err := scanFriendWithNoTotal(r.db.QueryRow(ctx, `
		WITH updated AS (
			UPDATE friendships
			SET
				remark = CASE WHEN $3 THEN NULLIF($4, '') ELSE remark END,
				friend_group_id = CASE WHEN $5 THEN NULLIF($6, '')::uuid ELSE friend_group_id END,
				is_dnd = COALESCE($7, is_dnd),
				version = version + 1
			WHERE user_id = $1::uuid
			  AND friend_user_id = $2::uuid
			  AND status = 'active'
			  AND deleted_at IS NULL
			RETURNING *
		)
		SELECT
			f.id,
			f.remark,
			f.friend_group_id,
			fg.name,
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
		FROM updated f
		JOIN users u ON u.id = f.friend_user_id
		LEFT JOIN friend_groups fg ON fg.id = f.friend_group_id AND fg.deleted_at IS NULL`,
		normalizeText(params.UserID),
		normalizeText(params.FriendUserID),
		params.RemarkSet,
		normalizeTextPtr(params.Remark),
		params.GroupIDSet,
		normalizeTextPtr(params.GroupID),
		params.IsDND,
	))
	return friend, translateWriteError(err)
}

func (r *FriendRepository) DeleteFriend(ctx context.Context, userID string, friendUserID string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE friendships
		SET
			status = 'deleted',
			deleted_at = now(),
			version = version + 1
		WHERE (
		    (user_id = $1::uuid AND friend_user_id = $2::uuid)
		    OR (user_id = $2::uuid AND friend_user_id = $1::uuid)
		  )
		  AND status = 'active'
		  AND deleted_at IS NULL`,
		normalizeText(userID),
		normalizeText(friendUserID),
	)
	if err != nil {
		return translateWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *FriendRepository) ActiveFriendshipExists(ctx context.Context, userID string, friendUserID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM friendships
			WHERE user_id = $1::uuid
			  AND friend_user_id = $2::uuid
			  AND status = 'active'
			  AND deleted_at IS NULL
		)`,
		normalizeText(userID),
		normalizeText(friendUserID),
	).Scan(&exists)
	return exists, translateReadError(err)
}

func (r *FriendRepository) CreateFriendRequest(ctx context.Context, params CreateFriendRequestParams) (FriendRequest, error) {
	var requestID pgtype.UUID
	err := r.db.QueryRow(ctx, `
		INSERT INTO friend_requests (
			from_user_id,
			to_user_id,
			message,
			source_type,
			source_group_id,
			source_friend_id,
			status
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6::uuid, 'pending')
		RETURNING id`,
		normalizeText(params.FromUserID),
		normalizeText(params.ToUserID),
		normalizeTextPtr(params.Message),
		normalizeTextPtr(params.SourceType),
		normalizeTextPtr(params.SourceGroupID),
		normalizeTextPtr(params.SourceFriendID),
	).Scan(&requestID)
	if err != nil {
		return FriendRequest{}, translateWriteError(err)
	}

	return r.GetFriendRequest(ctx, uuidString(requestID))
}

func (r *FriendRepository) GetPendingFriendRequest(ctx context.Context, fromUserID string, toUserID string) (FriendRequest, error) {
	requestID := ""
	err := r.db.QueryRow(ctx, `
		SELECT id
		FROM friend_requests
		WHERE from_user_id = $1::uuid
		  AND to_user_id = $2::uuid
		  AND status = 'pending'
		ORDER BY created_at DESC
		LIMIT 1`,
		normalizeText(fromUserID),
		normalizeText(toUserID),
	).Scan(&requestID)
	if err != nil {
		return FriendRequest{}, translateReadError(err)
	}

	return r.GetFriendRequest(ctx, requestID)
}

func (r *FriendRepository) GetFriendRequest(ctx context.Context, requestID string) (FriendRequest, error) {
	request, err := scanFriendRequest(r.db.QueryRow(ctx, friendRequestSelectSQL()+`
		WHERE fr.id = $1::uuid`,
		normalizeText(requestID),
	))
	return request, translateReadError(err)
}

func (r *FriendRepository) ListFriendRequests(ctx context.Context, userID string, status string, limit int, offset int) ([]FriendRequest, int, int, error) {
	rows, err := r.db.Query(ctx, friendRequestListSelectSQL()+`
		WHERE (fr.from_user_id = $1::uuid OR fr.to_user_id = $1::uuid)
		  AND ($2 = '' OR fr.status = $2)
		ORDER BY fr.created_at DESC
		LIMIT $3 OFFSET $4`,
		normalizeText(userID),
		normalizeText(status),
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, 0, translateReadError(err)
	}
	defer rows.Close()

	var total int
	var unread int
	requests, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (FriendRequest, error) {
		request, rowTotal, rowUnread, err := scanFriendRequestWithCounts(row)
		if err != nil {
			return FriendRequest{}, err
		}
		total = rowTotal
		unread = rowUnread
		return request, nil
	})
	if err != nil {
		return nil, 0, 0, translateReadError(err)
	}

	return requests, total, unread, nil
}

func (r *FriendRepository) AcceptFriendRequest(ctx context.Context, requestID string, toUserID string) (FriendRequest, error) {
	return r.updateFriendRequestStatus(ctx, requestID, toUserID, "accepted")
}

func (r *FriendRepository) RejectFriendRequest(ctx context.Context, requestID string, toUserID string) (FriendRequest, error) {
	return r.updateFriendRequestStatus(ctx, requestID, toUserID, "rejected")
}

func (r *FriendRepository) UpsertFriendship(ctx context.Context, params UpsertFriendshipParams) (Friend, error) {
	friend, err := scanFriendWithNoTotal(r.db.QueryRow(ctx, `
		WITH upserted AS (
			INSERT INTO friendships (
				user_id,
				friend_user_id,
				remark,
				friend_group_id,
				is_dnd,
				status,
				deleted_at
			)
			VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5, 'active', NULL)
			ON CONFLICT (user_id, friend_user_id) DO UPDATE
			SET
				remark = EXCLUDED.remark,
				friend_group_id = EXCLUDED.friend_group_id,
				is_dnd = EXCLUDED.is_dnd,
				status = 'active',
				deleted_at = NULL,
				version = friendships.version + 1
			RETURNING *
		)
		SELECT
			f.id,
			f.remark,
			f.friend_group_id,
			fg.name,
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
		FROM upserted f
		JOIN users u ON u.id = f.friend_user_id
		LEFT JOIN friend_groups fg ON fg.id = f.friend_group_id AND fg.deleted_at IS NULL`,
		normalizeText(params.UserID),
		normalizeText(params.FriendUserID),
		normalizeTextPtr(params.Remark),
		normalizeTextPtr(params.FriendGroupID),
		params.IsDND,
	))
	return friend, translateWriteError(err)
}

func (r *FriendRepository) updateFriendRequestStatus(ctx context.Context, requestID string, toUserID string, status string) (FriendRequest, error) {
	var updatedID pgtype.UUID
	err := r.db.QueryRow(ctx, `
		UPDATE friend_requests
		SET
			status = $3,
			handled_at = now()
		WHERE id = $1::uuid
		  AND to_user_id = $2::uuid
		  AND status = 'pending'
		RETURNING id`,
		normalizeText(requestID),
		normalizeText(toUserID),
		status,
	).Scan(&updatedID)
	if err != nil {
		return FriendRequest{}, translateWriteError(err)
	}

	return r.GetFriendRequest(ctx, uuidString(updatedID))
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

func friendRequestSelectSQL() string {
	return `
		SELECT
			fr.id,
			fr.message,
			fr.source_type,
			fr.source_group_id,
			fr.source_friend_id,
			fr.status,
			fr.handled_at,
			fr.created_at,
			fr.updated_at,
			from_user.id,
			from_user.public_id,
			from_user.email,
			from_user.password_hash,
			from_user.nick,
			from_user.gender,
			from_user.gender_custom,
			from_user.avatar_file_id,
			from_user.status,
			from_user.signature,
			from_user.region,
			from_user.last_seen_at,
			from_user.token_version,
			from_user.version,
			from_user.created_at,
			from_user.updated_at,
			from_user.deleted_at,
			to_user.id,
			to_user.public_id,
			to_user.email,
			to_user.password_hash,
			to_user.nick,
			to_user.gender,
			to_user.gender_custom,
			to_user.avatar_file_id,
			to_user.status,
			to_user.signature,
			to_user.region,
			to_user.last_seen_at,
			to_user.token_version,
			to_user.version,
			to_user.created_at,
			to_user.updated_at,
			to_user.deleted_at
		FROM friend_requests fr
		JOIN users from_user ON from_user.id = fr.from_user_id
		JOIN users to_user ON to_user.id = fr.to_user_id
		`
}

func friendRequestListSelectSQL() string {
	return `
		SELECT
			fr.id,
			fr.message,
			fr.source_type,
			fr.source_group_id,
			fr.source_friend_id,
			fr.status,
			fr.handled_at,
			fr.created_at,
			fr.updated_at,
			from_user.id,
			from_user.public_id,
			from_user.email,
			from_user.password_hash,
			from_user.nick,
			from_user.gender,
			from_user.gender_custom,
			from_user.avatar_file_id,
			from_user.status,
			from_user.signature,
			from_user.region,
			from_user.last_seen_at,
			from_user.token_version,
			from_user.version,
			from_user.created_at,
			from_user.updated_at,
			from_user.deleted_at,
			to_user.id,
			to_user.public_id,
			to_user.email,
			to_user.password_hash,
			to_user.nick,
			to_user.gender,
			to_user.gender_custom,
			to_user.avatar_file_id,
			to_user.status,
			to_user.signature,
			to_user.region,
			to_user.last_seen_at,
			to_user.token_version,
			to_user.version,
			to_user.created_at,
			to_user.updated_at,
			to_user.deleted_at,
			(count(*) OVER())::int AS total_count,
			(
				SELECT count(*)::int
				FROM friend_requests unread
				WHERE unread.to_user_id = $1::uuid
				  AND unread.status = 'pending'
			) AS unread_count
		FROM friend_requests fr
		JOIN users from_user ON from_user.id = fr.from_user_id
		JOIN users to_user ON to_user.id = fr.to_user_id
		`
}

func scanFriendRequest(row scanner) (FriendRequest, error) {
	request, _, _, err := scanFriendRequestRow(row, false)
	return request, err
}

func scanFriendRequestWithCounts(row scanner) (FriendRequest, int, int, error) {
	return scanFriendRequestRow(row, true)
}

func scanFriendRequestRow(row scanner, withCounts bool) (FriendRequest, int, int, error) {
	var (
		request        FriendRequest
		requestID      pgtype.UUID
		message        sql.NullString
		sourceType     sql.NullString
		sourceGroupID  pgtype.UUID
		sourceFriendID pgtype.UUID
		handledAt      sql.NullTime
		fromID         pgtype.UUID
		fromGender     sql.NullString
		fromAvatarID   pgtype.UUID
		fromSignature  sql.NullString
		fromRegion     sql.NullString
		fromLastSeen   sql.NullTime
		fromDeletedAt  sql.NullTime
		toID           pgtype.UUID
		toGender       sql.NullString
		toAvatarID     pgtype.UUID
		toSignature    sql.NullString
		toRegion       sql.NullString
		toLastSeen     sql.NullTime
		toDeletedAt    sql.NullTime
		total          int
		unread         int
	)

	dest := []any{
		&requestID,
		&message,
		&sourceType,
		&sourceGroupID,
		&sourceFriendID,
		&request.Status,
		&handledAt,
		&request.CreatedAt,
		&request.UpdatedAt,
		&fromID,
		&request.FromUser.PublicID,
		&request.FromUser.Email,
		&request.FromUser.PasswordHash,
		&request.FromUser.Nick,
		&request.FromUser.Gender,
		&fromGender,
		&fromAvatarID,
		&request.FromUser.Status,
		&fromSignature,
		&fromRegion,
		&fromLastSeen,
		&request.FromUser.TokenVersion,
		&request.FromUser.Version,
		&request.FromUser.CreatedAt,
		&request.FromUser.UpdatedAt,
		&fromDeletedAt,
		&toID,
		&request.ToUser.PublicID,
		&request.ToUser.Email,
		&request.ToUser.PasswordHash,
		&request.ToUser.Nick,
		&request.ToUser.Gender,
		&toGender,
		&toAvatarID,
		&request.ToUser.Status,
		&toSignature,
		&toRegion,
		&toLastSeen,
		&request.ToUser.TokenVersion,
		&request.ToUser.Version,
		&request.ToUser.CreatedAt,
		&request.ToUser.UpdatedAt,
		&toDeletedAt,
	}
	if withCounts {
		dest = append(dest, &total, &unread)
	}

	if err := row.Scan(dest...); err != nil {
		return FriendRequest{}, 0, 0, err
	}

	request.ID = uuidString(requestID)
	if message.Valid {
		request.Message = &message.String
	}
	if sourceType.Valid {
		request.SourceType = &sourceType.String
	}
	request.SourceGroupID = uuidStringPtr(sourceGroupID)
	request.SourceFriendID = uuidStringPtr(sourceFriendID)
	if handledAt.Valid {
		request.HandledAt = &handledAt.Time
	}

	request.FromUser.ID = uuidString(fromID)
	if fromGender.Valid {
		request.FromUser.GenderCustom = &fromGender.String
	}
	request.FromUser.AvatarFileID = uuidStringPtr(fromAvatarID)
	if fromSignature.Valid {
		request.FromUser.Signature = &fromSignature.String
	}
	if fromRegion.Valid {
		request.FromUser.Region = &fromRegion.String
	}
	if fromLastSeen.Valid {
		request.FromUser.LastSeenAt = &fromLastSeen.Time
	}
	if fromDeletedAt.Valid {
		request.FromUser.DeletedAt = &fromDeletedAt.Time
	}

	request.ToUser.ID = uuidString(toID)
	if toGender.Valid {
		request.ToUser.GenderCustom = &toGender.String
	}
	request.ToUser.AvatarFileID = uuidStringPtr(toAvatarID)
	if toSignature.Valid {
		request.ToUser.Signature = &toSignature.String
	}
	if toRegion.Valid {
		request.ToUser.Region = &toRegion.String
	}
	if toLastSeen.Valid {
		request.ToUser.LastSeenAt = &toLastSeen.Time
	}
	if toDeletedAt.Valid {
		request.ToUser.DeletedAt = &toDeletedAt.Time
	}

	return request, total, unread, nil
}

func scanFriend(row scanner, withTotal bool) (Friend, int, error) {
	var (
		friend          Friend
		friendID        pgtype.UUID
		remark          sql.NullString
		friendGroup     pgtype.UUID
		friendGroupName sql.NullString
		userID          pgtype.UUID
		genderCustom    sql.NullString
		avatarFileID    pgtype.UUID
		signature       sql.NullString
		region          sql.NullString
		lastSeenAt      sql.NullTime
		deletedAt       sql.NullTime
		total           int
	)

	dest := []any{
		&friendID,
		&remark,
		&friendGroup,
		&friendGroupName,
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
	if friendGroupName.Valid {
		friend.FriendGroupName = &friendGroupName.String
	}
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
