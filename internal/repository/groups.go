package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type GroupRepository struct {
	db DBTX
}

const groupJoinRequestSelectColumns = `
	gjr.id,
	g.id,
	g.public_id,
	g.name,
	g.owner_id,
	g.avatar_file_id,
	g.avatar_version,
	g.introduction,
	g.announcement,
	g.version,
	g.created_at,
	g.updated_at,
	actor.id,
	actor.public_id,
	actor.email,
	actor.password_hash,
	actor.nick,
	actor.gender,
	actor.gender_custom,
	actor.avatar_file_id,
	actor.status,
	actor.signature,
	actor.region,
	actor.last_seen_at,
	actor.token_version,
	actor.version,
	actor.created_at,
	actor.updated_at,
	actor.deleted_at,
	op.id,
	op.public_id,
	op.email,
	op.password_hash,
	op.nick,
	op.gender,
	op.gender_custom,
	op.avatar_file_id,
	op.status,
	op.signature,
	op.region,
	op.last_seen_at,
	op.token_version,
	op.version,
	op.created_at,
	op.updated_at,
	op.deleted_at,
	gjr.message,
	gjr.status,
	gjr.handled_at,
	gjr.created_at,
	gjr.updated_at`

type Group struct {
	ID            string
	PublicID      string
	Name          string
	OwnerID       string
	AvatarFileID  *string
	AvatarVersion int
	Introduction  *string
	Announcement  *string
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type GroupListEntry struct {
	Group         Group
	Role          string
	Nickname      *string
	IsDND         bool
	Remark        *string
	ListGroupID   *string
	ListGroupName *string
	MemberCount   int
}

type GroupMember struct {
	GroupID   string
	User      User
	Role      string
	Nickname  *string
	IsDND     bool
	JoinedAt  time.Time
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type GroupUserSettings struct {
	GroupID       string
	UserID        string
	Remark        *string
	ListGroupID   *string
	ListGroupName *string
	IsDND         bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type GroupJoinRequest struct {
	ID           string
	Group        Group
	ActorUser    User
	OperatorUser *User
	Message      *string
	Status       string
	HandledAt    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateGroupParams struct {
	PublicID     string
	Name         string
	OwnerID      string
	AvatarFileID *string
}

type UpdateGroupParams struct {
	GroupID         string
	NameSet         bool
	Name            *string
	IntroductionSet bool
	Introduction    *string
	AnnouncementSet bool
	Announcement    *string
	AvatarFileIDSet bool
	AvatarFileID    *string
}

type UpdateGroupMemberParams struct {
	GroupID     string
	UserID      string
	RoleSet     bool
	Role        *string
	NicknameSet bool
	Nickname    *string
	IsDND       *bool
}

type UpsertGroupUserSettingsParams struct {
	GroupID          string
	UserID           string
	RemarkSet        bool
	Remark           *string
	ListGroupIDSet   bool
	ListGroupID      *string
	ListGroupNameSet bool
	ListGroupName    *string
	IsDND            *bool
}

func NewGroupRepository(db DBTX) *GroupRepository {
	return &GroupRepository{db: db}
}

func (r *GroupRepository) Create(ctx context.Context, params CreateGroupParams) (Group, error) {
	group, err := scanGroup(r.db.QueryRow(ctx, `
		INSERT INTO groups (public_id, name, owner_id, avatar_file_id)
		VALUES ($1, $2, $3::uuid, $4::uuid)
		RETURNING
			id,
			public_id,
			name,
			owner_id,
			avatar_file_id,
			avatar_version,
			introduction,
			announcement,
			version,
			created_at,
			updated_at`,
		normalizeText(params.PublicID),
		normalizeText(params.Name),
		normalizeText(params.OwnerID),
		normalizeTextPtr(params.AvatarFileID),
	))
	return group, translateWriteError(err)
}

func (r *GroupRepository) Get(ctx context.Context, groupID string) (Group, error) {
	group, err := scanGroup(r.db.QueryRow(ctx, `
		SELECT
			id,
			public_id,
			name,
			owner_id,
			avatar_file_id,
			avatar_version,
			introduction,
			announcement,
			version,
			created_at,
			updated_at
		FROM groups
		WHERE id = $1::uuid
		  AND deleted_at IS NULL`,
		normalizeText(groupID),
	))
	return group, translateReadError(err)
}

func (r *GroupRepository) GetByPublicID(ctx context.Context, publicID string) (Group, error) {
	group, err := scanGroup(r.db.QueryRow(ctx, `
		SELECT
			id,
			public_id,
			name,
			owner_id,
			avatar_file_id,
			avatar_version,
			introduction,
			announcement,
			version,
			created_at,
			updated_at
		FROM groups
		WHERE public_id = $1
		  AND deleted_at IS NULL`,
		normalizeText(publicID),
	))
	return group, translateReadError(err)
}

func (r *GroupRepository) PublicIDExists(ctx context.Context, publicID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM groups
			WHERE public_id = $1
			  AND deleted_at IS NULL
		)`,
		normalizeText(publicID),
	).Scan(&exists)
	return exists, translateReadError(err)
}

func (r *GroupRepository) Update(ctx context.Context, params UpdateGroupParams) (Group, error) {
	group, err := scanGroup(r.db.QueryRow(ctx, `
		UPDATE groups
		SET
			name = CASE WHEN $2 THEN $3 ELSE name END,
			introduction = CASE WHEN $4 THEN $5 ELSE introduction END,
			announcement = CASE WHEN $6 THEN $7 ELSE announcement END,
			avatar_file_id = CASE WHEN $8 THEN $9::uuid ELSE avatar_file_id END,
			avatar_version = CASE
				WHEN $8 AND avatar_file_id IS DISTINCT FROM $9::uuid THEN avatar_version + 1
				ELSE avatar_version
			END,
			version = CASE
				WHEN ($2 AND name IS DISTINCT FROM $3)
				  OR ($4 AND introduction IS DISTINCT FROM $5)
				  OR ($6 AND announcement IS DISTINCT FROM $7)
				  OR ($8 AND avatar_file_id IS DISTINCT FROM $9::uuid)
				THEN version + 1
				ELSE version
			END
		WHERE id = $1::uuid
		  AND deleted_at IS NULL
		RETURNING
			id,
			public_id,
			name,
			owner_id,
			avatar_file_id,
			avatar_version,
			introduction,
			announcement,
			version,
			created_at,
			updated_at`,
		normalizeText(params.GroupID),
		params.NameSet,
		normalizeTextPtr(params.Name),
		params.IntroductionSet,
		normalizeTextPtr(params.Introduction),
		params.AnnouncementSet,
		normalizeTextPtr(params.Announcement),
		params.AvatarFileIDSet,
		normalizeTextPtr(params.AvatarFileID),
	))
	return group, translateWriteError(err)
}

func (r *GroupRepository) SoftDelete(ctx context.Context, groupID string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE groups
		SET deleted_at = now(), version = version + 1
		WHERE id = $1::uuid
		  AND deleted_at IS NULL`,
		normalizeText(groupID),
	)
	if err != nil {
		return translateWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *GroupRepository) GetMember(ctx context.Context, groupID string, userID string) (GroupMember, error) {
	member, _, err := scanGroupMemberWithTotal(r.db.QueryRow(ctx, `
		SELECT
			gm.group_id,
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
			gm.role,
			gm.nickname,
			gm.is_dnd,
			gm.joined_at,
			gm.version,
			gm.created_at,
			gm.updated_at,
			1 AS total_count
		FROM group_members gm
		JOIN users u ON u.id = gm.user_id
		JOIN groups g ON g.id = gm.group_id
		WHERE gm.group_id = $1::uuid
		  AND gm.user_id = $2::uuid
		  AND g.deleted_at IS NULL
		  AND u.deleted_at IS NULL`,
		normalizeText(groupID),
		normalizeText(userID),
	))
	return member, translateReadError(err)
}

func (r *GroupRepository) AddMembers(ctx context.Context, groupID string, userIDs []string, role string) error {
	for _, userID := range userIDs {
		tag, err := r.db.Exec(ctx, `
			INSERT INTO group_members (group_id, user_id, role)
			SELECT $1::uuid, u.id, $3
			FROM users u
			WHERE u.id = $2::uuid
			  AND u.deleted_at IS NULL
			  AND u.status = 'active'
			ON CONFLICT (group_id, user_id) DO NOTHING`,
			normalizeText(groupID),
			normalizeText(userID),
			normalizeText(role),
		)
		if err != nil {
			return translateWriteError(err)
		}
		if tag.RowsAffected() == 0 {
			if _, err := r.GetMember(ctx, groupID, userID); err == nil {
				continue
			}
			return ErrNotFound
		}
	}
	return nil
}

func (r *GroupRepository) RemoveMember(ctx context.Context, groupID string, userID string) error {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM group_members
		WHERE group_id = $1::uuid
		  AND user_id = $2::uuid`,
		normalizeText(groupID),
		normalizeText(userID),
	)
	if err != nil {
		return translateWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *GroupRepository) UpdateMember(ctx context.Context, params UpdateGroupMemberParams) (GroupMember, error) {
	member, _, err := scanGroupMemberWithTotal(r.db.QueryRow(ctx, `
		WITH updated AS (
			UPDATE group_members
			SET
				role = CASE WHEN $3 THEN $4 ELSE role END,
				nickname = CASE WHEN $5 THEN $6 ELSE nickname END,
				is_dnd = COALESCE($7, is_dnd),
				version = version + 1
			WHERE group_id = $1::uuid
			  AND user_id = $2::uuid
			RETURNING *
		)
		SELECT
			updated.group_id,
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
			updated.role,
			updated.nickname,
			updated.is_dnd,
			updated.joined_at,
			updated.version,
			updated.created_at,
			updated.updated_at,
			1 AS total_count
		FROM updated
		JOIN users u ON u.id = updated.user_id`,
		normalizeText(params.GroupID),
		normalizeText(params.UserID),
		params.RoleSet,
		normalizeTextPtr(params.Role),
		params.NicknameSet,
		normalizeTextPtr(params.Nickname),
		params.IsDND,
	))
	return member, translateWriteError(err)
}

func (r *GroupRepository) UpdateOwner(ctx context.Context, groupID string, ownerID string) (Group, error) {
	group, err := scanGroup(r.db.QueryRow(ctx, `
		UPDATE groups
		SET owner_id = $2::uuid,
		    version = version + 1
		WHERE id = $1::uuid
		  AND deleted_at IS NULL
		RETURNING
			id,
			public_id,
			name,
			owner_id,
			avatar_file_id,
			avatar_version,
			introduction,
			announcement,
			version,
			created_at,
			updated_at`,
		normalizeText(groupID),
		normalizeText(ownerID),
	))
	return group, translateWriteError(err)
}

func (r *GroupRepository) UpsertUserSettings(ctx context.Context, params UpsertGroupUserSettingsParams) (GroupUserSettings, error) {
	settings, err := scanGroupUserSettings(r.db.QueryRow(ctx, `
		INSERT INTO group_user_settings (
			group_id,
			user_id,
			remark,
			list_group_id,
			list_group_name,
			is_dnd
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			CASE WHEN $3 THEN $4 ELSE NULL END,
			CASE WHEN $5 THEN $6::uuid ELSE NULL END,
			CASE WHEN $7 THEN $8 ELSE NULL END,
			COALESCE($9, false)
		)
		ON CONFLICT (group_id, user_id) DO UPDATE
		SET
			remark = CASE WHEN $3 THEN $4 ELSE group_user_settings.remark END,
			list_group_id = CASE WHEN $5 THEN $6::uuid ELSE group_user_settings.list_group_id END,
			list_group_name = CASE WHEN $7 THEN $8 ELSE group_user_settings.list_group_name END,
			is_dnd = COALESCE($9, group_user_settings.is_dnd)
		RETURNING
			group_id,
			user_id,
			remark,
			list_group_id,
			list_group_name,
			is_dnd,
			created_at,
			updated_at`,
		normalizeText(params.GroupID),
		normalizeText(params.UserID),
		params.RemarkSet,
		normalizeTextPtr(params.Remark),
		params.ListGroupIDSet,
		normalizeTextPtr(params.ListGroupID),
		params.ListGroupNameSet,
		normalizeTextPtr(params.ListGroupName),
		params.IsDND,
	))
	return settings, translateWriteError(err)
}

func (r *GroupRepository) CreateJoinRequest(ctx context.Context, groupID string, actorUserID string, message *string) (GroupJoinRequest, error) {
	request, err := scanGroupJoinRequest(r.db.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO group_join_requests (group_id, actor_user_id, message)
			VALUES ($1::uuid, $2::uuid, $3)
			RETURNING *
		)
		SELECT `+groupJoinRequestSelectColumns+`
		FROM inserted gjr
		JOIN groups g ON g.id = gjr.group_id
		JOIN users actor ON actor.id = gjr.actor_user_id
		LEFT JOIN users op ON op.id = gjr.operator_user_id`,
		normalizeText(groupID),
		normalizeText(actorUserID),
		normalizeTextPtr(message),
	))
	return request, translateWriteError(err)
}

func (r *GroupRepository) GetPendingJoinRequest(ctx context.Context, groupID string, actorUserID string) (GroupJoinRequest, error) {
	request, err := scanGroupJoinRequest(r.db.QueryRow(ctx, `
		SELECT `+groupJoinRequestSelectColumns+`
		FROM group_join_requests gjr
		JOIN groups g ON g.id = gjr.group_id
		JOIN users actor ON actor.id = gjr.actor_user_id
		LEFT JOIN users op ON op.id = gjr.operator_user_id
		WHERE gjr.group_id = $1::uuid
		  AND gjr.actor_user_id = $2::uuid
		  AND gjr.status = 'pending'
		  AND g.deleted_at IS NULL`,
		normalizeText(groupID),
		normalizeText(actorUserID),
	))
	return request, translateReadError(err)
}

func (r *GroupRepository) ListJoinRequests(ctx context.Context, userID string, limit int, offset int) ([]GroupJoinRequest, int, int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+groupJoinRequestSelectColumns+`,
		       (count(*) OVER())::int AS total_count,
		       (count(*) FILTER (WHERE gjr.status = 'pending') OVER())::int AS unread_count
		FROM group_join_requests gjr
		JOIN groups g ON g.id = gjr.group_id
		JOIN users actor ON actor.id = gjr.actor_user_id
		LEFT JOIN users op ON op.id = gjr.operator_user_id
		WHERE g.deleted_at IS NULL
		  AND EXISTS (
		    SELECT 1
		    FROM group_members gm
		    WHERE gm.group_id = gjr.group_id
		      AND gm.user_id = $1::uuid
		      AND gm.role IN ('owner', 'admin')
		  )
		ORDER BY gjr.created_at DESC
		LIMIT $2 OFFSET $3`,
		normalizeText(userID),
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, 0, translateReadError(err)
	}
	defer rows.Close()

	var total int
	var unread int
	requests, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupJoinRequest, error) {
		request, rowTotal, rowUnread, err := scanGroupJoinRequestWithCounts(row)
		if err != nil {
			return GroupJoinRequest{}, err
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

func (r *GroupRepository) AcceptJoinRequest(ctx context.Context, requestID string, operatorUserID string) (GroupJoinRequest, error) {
	request, err := scanGroupJoinRequest(r.db.QueryRow(ctx, `
		WITH updated AS (
			UPDATE group_join_requests gjr
			SET status = 'accepted',
			    operator_user_id = $2::uuid,
			    handled_at = now()
			WHERE gjr.id = $1::uuid
			  AND gjr.status = 'pending'
			  AND EXISTS (
			    SELECT 1
			    FROM group_members gm
			    WHERE gm.group_id = gjr.group_id
			      AND gm.user_id = $2::uuid
			      AND gm.role IN ('owner', 'admin')
			  )
			RETURNING *
		)
		SELECT `+groupJoinRequestSelectColumns+`
		FROM updated gjr
		JOIN groups g ON g.id = gjr.group_id
		JOIN users actor ON actor.id = gjr.actor_user_id
		LEFT JOIN users op ON op.id = gjr.operator_user_id`,
		normalizeText(requestID),
		normalizeText(operatorUserID),
	))
	return request, translateWriteError(err)
}

func (r *GroupRepository) RejectJoinRequest(ctx context.Context, requestID string, operatorUserID string) (GroupJoinRequest, error) {
	request, err := scanGroupJoinRequest(r.db.QueryRow(ctx, `
		WITH updated AS (
			UPDATE group_join_requests gjr
			SET status = 'rejected',
			    operator_user_id = $2::uuid,
			    handled_at = now()
			WHERE gjr.id = $1::uuid
			  AND gjr.status = 'pending'
			  AND EXISTS (
			    SELECT 1
			    FROM group_members gm
			    WHERE gm.group_id = gjr.group_id
			      AND gm.user_id = $2::uuid
			      AND gm.role IN ('owner', 'admin')
			  )
			RETURNING *
		)
		SELECT `+groupJoinRequestSelectColumns+`
		FROM updated gjr
		JOIN groups g ON g.id = gjr.group_id
		JOIN users actor ON actor.id = gjr.actor_user_id
		LEFT JOIN users op ON op.id = gjr.operator_user_id`,
		normalizeText(requestID),
		normalizeText(operatorUserID),
	))
	return request, translateWriteError(err)
}

func (r *GroupRepository) AddEvent(ctx context.Context, groupID string, eventType string, actorUserID *string, operatorUserID *string, payload json.RawMessage) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO group_events (
			group_id,
			type,
			actor_user_id,
			operator_user_id,
			payload_json
		)
		VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5::jsonb)`,
		normalizeText(groupID),
		normalizeText(eventType),
		normalizeTextPtr(actorUserID),
		normalizeTextPtr(operatorUserID),
		payload,
	)
	return translateWriteError(err)
}

func (r *GroupRepository) ListForUser(ctx context.Context, userID string, keyword string, categoryID string, roleFilter string, limit int, offset int) ([]GroupListEntry, int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			g.id,
			g.public_id,
			g.name,
			g.owner_id,
			g.avatar_file_id,
			g.avatar_version,
			g.introduction,
			g.announcement,
			g.version,
			g.created_at,
			g.updated_at,
			gm.role,
			gm.nickname,
			gm.is_dnd,
			gus.remark,
			gus.list_group_id,
			gus.list_group_name,
			(
			  SELECT count(*)::int
			  FROM group_members member_count
			  WHERE member_count.group_id = g.id
			) AS member_count,
			(count(*) OVER())::int AS total_count
		FROM group_members gm
		JOIN groups g ON g.id = gm.group_id
		LEFT JOIN group_user_settings gus
		  ON gus.group_id = g.id
		 AND gus.user_id = $1::uuid
		WHERE gm.user_id = $1::uuid
		  AND g.deleted_at IS NULL
		  AND ($2 = '' OR g.public_id ILIKE '%' || $2 || '%' OR g.name ILIKE '%' || $2 || '%' OR gus.remark ILIKE '%' || $2 || '%')
		  AND ($3 = '' OR gus.list_group_id = NULLIF($3, '')::uuid)
		  AND (
		    $4 = ''
		    OR $4 = 'joined'
		    OR ($4 IN ('created', 'owner') AND gm.role = 'owner')
		    OR ($4 = 'admin' AND gm.role = 'admin')
		    OR ($4 = 'managed' AND gm.role IN ('owner', 'admin'))
		    OR ($4 = 'member' AND gm.role = 'member')
		  )
		ORDER BY COALESCE(gus.remark, g.name) ASC, g.created_at DESC
		LIMIT $5 OFFSET $6`,
		normalizeText(userID),
		normalizeText(keyword),
		normalizeText(categoryID),
		normalizeText(roleFilter),
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, translateReadError(err)
	}
	defer rows.Close()

	var total int
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupListEntry, error) {
		entry, rowTotal, err := scanGroupListEntry(row)
		if err != nil {
			return GroupListEntry{}, err
		}
		total = rowTotal
		return entry, nil
	})
	if err != nil {
		return nil, 0, translateReadError(err)
	}

	return entries, total, nil
}

func (r *GroupRepository) GetForUser(ctx context.Context, userID string, groupID string) (GroupListEntry, error) {
	entry, _, err := scanGroupListEntry(r.db.QueryRow(ctx, `
		SELECT
			g.id,
			g.public_id,
			g.name,
			g.owner_id,
			g.avatar_file_id,
			g.avatar_version,
			g.introduction,
			g.announcement,
			g.version,
			g.created_at,
			g.updated_at,
			gm.role,
			gm.nickname,
			gm.is_dnd,
			gus.remark,
			gus.list_group_id,
			gus.list_group_name,
			(
			  SELECT count(*)::int
			  FROM group_members member_count
			  WHERE member_count.group_id = g.id
			) AS member_count,
			1 AS total_count
		FROM group_members gm
		JOIN groups g ON g.id = gm.group_id
		LEFT JOIN group_user_settings gus
		  ON gus.group_id = g.id
		 AND gus.user_id = $1::uuid
		WHERE gm.user_id = $1::uuid
		  AND g.id = $2::uuid
		  AND g.deleted_at IS NULL`,
		normalizeText(userID),
		normalizeText(groupID),
	))
	return entry, translateReadError(err)
}

func (r *GroupRepository) GetByPublicIDForUser(ctx context.Context, userID string, publicID string) (GroupListEntry, error) {
	entry, _, err := scanGroupListEntry(r.db.QueryRow(ctx, `
		SELECT
			g.id,
			g.public_id,
			g.name,
			g.owner_id,
			g.avatar_file_id,
			g.avatar_version,
			g.introduction,
			g.announcement,
			g.version,
			g.created_at,
			g.updated_at,
			COALESCE(gm.role, '') AS role,
			gm.nickname,
			COALESCE(gm.is_dnd, false) AS is_dnd,
			gus.remark,
			gus.list_group_id,
			gus.list_group_name,
			(
			  SELECT count(*)::int
			  FROM group_members member_count
			  WHERE member_count.group_id = g.id
			) AS member_count,
			1 AS total_count
		FROM groups g
		LEFT JOIN group_members gm
		  ON gm.group_id = g.id
		 AND gm.user_id = $1::uuid
		LEFT JOIN group_user_settings gus
		  ON gus.group_id = g.id
		 AND gus.user_id = $1::uuid
		WHERE g.public_id = $2
		  AND g.deleted_at IS NULL`,
		normalizeText(userID),
		normalizeText(publicID),
	))
	return entry, translateReadError(err)
}

func (r *GroupRepository) ListMembers(ctx context.Context, userID string, groupID string, keyword string, limit int, offset int) ([]GroupMember, int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			gm.group_id,
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
			gm.role,
			gm.nickname,
			gm.is_dnd,
			gm.joined_at,
			gm.version,
			gm.created_at,
			gm.updated_at,
			(count(*) OVER())::int AS total_count
		FROM group_members gm
		JOIN users u ON u.id = gm.user_id
		WHERE gm.group_id = $2::uuid
		  AND u.deleted_at IS NULL
		  AND u.status = 'active'
		  AND EXISTS (
		    SELECT 1
		    FROM group_members viewer
		    WHERE viewer.group_id = gm.group_id
		      AND viewer.user_id = $1::uuid
		  )
		  AND ($3 = '' OR u.public_id ILIKE '%' || $3 || '%' OR u.nick ILIKE '%' || $3 || '%' OR gm.nickname ILIKE '%' || $3 || '%')
		ORDER BY
		  CASE gm.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END,
		  gm.joined_at ASC
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
	members, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupMember, error) {
		member, rowTotal, err := scanGroupMemberWithTotal(row)
		if err != nil {
			return GroupMember{}, err
		}
		total = rowTotal
		return member, nil
	})
	if err != nil {
		return nil, 0, translateReadError(err)
	}

	return members, total, nil
}

func (r *GroupRepository) BatchGetMembers(ctx context.Context, viewerUserID string, groupID string, userIDs []string) ([]GroupMember, error) {
	normalized := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		userID = normalizeText(userID)
		if userID != "" {
			normalized = append(normalized, userID)
		}
	}
	if len(normalized) == 0 {
		return []GroupMember{}, nil
	}

	placeholders := make([]string, 0, len(normalized))
	args := make([]any, 0, len(normalized)+2)
	args = append(args, normalizeText(viewerUserID), normalizeText(groupID))
	for i, userID := range normalized {
		placeholders = append(placeholders, fmt.Sprintf("($%d::uuid, %d)", i+3, i+1))
		args = append(args, userID)
	}

	rows, err := r.db.Query(ctx, `
		WITH input_ids(id, ord) AS (
			VALUES `+strings.Join(placeholders, ",")+`
		)
		SELECT
			gm.group_id,
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
			gm.role,
			gm.nickname,
			gm.is_dnd,
			gm.joined_at,
			gm.version,
			gm.created_at,
			gm.updated_at,
			1 AS total_count
		FROM input_ids
		JOIN group_members gm
		  ON gm.user_id = input_ids.id
		 AND gm.group_id = $2::uuid
		JOIN users u ON u.id = gm.user_id
		JOIN groups g ON g.id = gm.group_id
		WHERE g.deleted_at IS NULL
		  AND u.deleted_at IS NULL
		  AND u.status = 'active'
		  AND EXISTS (
		    SELECT 1
		    FROM group_members viewer
		    WHERE viewer.group_id = gm.group_id
		      AND viewer.user_id = $1::uuid
		  )
		ORDER BY input_ids.ord`,
		args...,
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	members, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupMember, error) {
		member, _, err := scanGroupMemberWithTotal(row)
		return member, err
	})
	if err != nil {
		return nil, translateReadError(err)
	}
	return members, nil
}

func (r *GroupRepository) ListActiveMemberUserIDs(ctx context.Context, groupID string) ([]string, error) {
	return r.listMemberUserIDsByRole(ctx, groupID, nil)
}

func (r *GroupRepository) ListCoMemberUserIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT gm2.user_id
		FROM group_members gm1
		JOIN groups g
		  ON g.id = gm1.group_id
		 AND g.deleted_at IS NULL
		JOIN group_members gm2 ON gm2.group_id = gm1.group_id
		JOIN users u
		  ON u.id = gm2.user_id
		 AND u.deleted_at IS NULL
		 AND u.status = 'active'
		WHERE gm1.user_id = $1::uuid
		ORDER BY gm2.user_id`,
		normalizeText(userID),
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	userIDs := []string{}
	for rows.Next() {
		var memberUserID pgtype.UUID
		if err := rows.Scan(&memberUserID); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, uuidString(memberUserID))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return userIDs, nil
}

func (r *GroupRepository) ListPrivilegedMemberUserIDs(ctx context.Context, groupID string) ([]string, error) {
	return r.listMemberUserIDsByRole(ctx, groupID, []string{"owner", "admin"})
}

func (r *GroupRepository) listMemberUserIDsByRole(ctx context.Context, groupID string, roles []string) ([]string, error) {
	roleFilter := ""
	args := []any{normalizeText(groupID)}
	if len(roles) > 0 {
		roleFilter = " AND role = ANY($2::text[])"
		args = append(args, roles)
	}

	rows, err := r.db.Query(ctx, `
		SELECT user_id
		FROM group_members
		WHERE group_id = $1::uuid`+roleFilter+`
		ORDER BY user_id`,
		args...,
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	userIDs := []string{}
	for rows.Next() {
		var userID pgtype.UUID
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, uuidString(userID))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return userIDs, nil
}

func scanGroupListEntry(row scanner) (GroupListEntry, int, error) {
	var (
		entry         GroupListEntry
		groupID       pgtype.UUID
		ownerID       pgtype.UUID
		avatarFileID  pgtype.UUID
		introduction  sql.NullString
		announcement  sql.NullString
		nickname      sql.NullString
		remark        sql.NullString
		listGroupID   pgtype.UUID
		listGroupName sql.NullString
		total         int
	)

	err := row.Scan(
		&groupID,
		&entry.Group.PublicID,
		&entry.Group.Name,
		&ownerID,
		&avatarFileID,
		&entry.Group.AvatarVersion,
		&introduction,
		&announcement,
		&entry.Group.Version,
		&entry.Group.CreatedAt,
		&entry.Group.UpdatedAt,
		&entry.Role,
		&nickname,
		&entry.IsDND,
		&remark,
		&listGroupID,
		&listGroupName,
		&entry.MemberCount,
		&total,
	)
	if err != nil {
		return GroupListEntry{}, 0, err
	}

	entry.Group.ID = uuidString(groupID)
	entry.Group.OwnerID = uuidString(ownerID)
	entry.Group.AvatarFileID = uuidStringPtr(avatarFileID)
	if introduction.Valid {
		entry.Group.Introduction = &introduction.String
	}
	if announcement.Valid {
		entry.Group.Announcement = &announcement.String
	}
	if nickname.Valid {
		entry.Nickname = &nickname.String
	}
	if remark.Valid {
		entry.Remark = &remark.String
	}
	entry.ListGroupID = uuidStringPtr(listGroupID)
	if listGroupName.Valid {
		entry.ListGroupName = &listGroupName.String
	}

	return entry, total, nil
}

func scanGroup(row scanner) (Group, error) {
	var (
		group        Group
		groupID      pgtype.UUID
		ownerID      pgtype.UUID
		avatarFileID pgtype.UUID
		introduction sql.NullString
		announcement sql.NullString
	)

	err := row.Scan(
		&groupID,
		&group.PublicID,
		&group.Name,
		&ownerID,
		&avatarFileID,
		&group.AvatarVersion,
		&introduction,
		&announcement,
		&group.Version,
		&group.CreatedAt,
		&group.UpdatedAt,
	)
	if err != nil {
		return Group{}, err
	}

	group.ID = uuidString(groupID)
	group.OwnerID = uuidString(ownerID)
	group.AvatarFileID = uuidStringPtr(avatarFileID)
	if introduction.Valid {
		group.Introduction = &introduction.String
	}
	if announcement.Valid {
		group.Announcement = &announcement.String
	}
	return group, nil
}

func scanGroupMemberWithTotal(row scanner) (GroupMember, int, error) {
	var (
		member       GroupMember
		groupID      pgtype.UUID
		userID       pgtype.UUID
		genderCustom sql.NullString
		avatarFileID pgtype.UUID
		signature    sql.NullString
		region       sql.NullString
		lastSeenAt   sql.NullTime
		deletedAt    sql.NullTime
		nickname     sql.NullString
		total        int
	)

	err := row.Scan(
		&groupID,
		&userID,
		&member.User.PublicID,
		&member.User.Email,
		&member.User.PasswordHash,
		&member.User.Nick,
		&member.User.Gender,
		&genderCustom,
		&avatarFileID,
		&member.User.Status,
		&signature,
		&region,
		&lastSeenAt,
		&member.User.TokenVersion,
		&member.User.Version,
		&member.User.CreatedAt,
		&member.User.UpdatedAt,
		&deletedAt,
		&member.Role,
		&nickname,
		&member.IsDND,
		&member.JoinedAt,
		&member.Version,
		&member.CreatedAt,
		&member.UpdatedAt,
		&total,
	)
	if err != nil {
		return GroupMember{}, 0, err
	}

	member.GroupID = uuidString(groupID)
	member.User.ID = uuidString(userID)
	if genderCustom.Valid {
		member.User.GenderCustom = &genderCustom.String
	}
	member.User.AvatarFileID = uuidStringPtr(avatarFileID)
	if signature.Valid {
		member.User.Signature = &signature.String
	}
	if region.Valid {
		member.User.Region = &region.String
	}
	if lastSeenAt.Valid {
		member.User.LastSeenAt = &lastSeenAt.Time
	}
	if deletedAt.Valid {
		member.User.DeletedAt = &deletedAt.Time
	}
	if nickname.Valid {
		member.Nickname = &nickname.String
	}

	return member, total, nil
}

func scanGroupUserSettings(row scanner) (GroupUserSettings, error) {
	var (
		settings      GroupUserSettings
		groupID       pgtype.UUID
		userID        pgtype.UUID
		remark        sql.NullString
		listGroupID   pgtype.UUID
		listGroupName sql.NullString
	)

	err := row.Scan(
		&groupID,
		&userID,
		&remark,
		&listGroupID,
		&listGroupName,
		&settings.IsDND,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	)
	if err != nil {
		return GroupUserSettings{}, err
	}

	settings.GroupID = uuidString(groupID)
	settings.UserID = uuidString(userID)
	if remark.Valid {
		settings.Remark = &remark.String
	}
	settings.ListGroupID = uuidStringPtr(listGroupID)
	if listGroupName.Valid {
		settings.ListGroupName = &listGroupName.String
	}
	return settings, nil
}

func scanGroupJoinRequest(row scanner) (GroupJoinRequest, error) {
	state := groupJoinRequestScanState{}
	if err := row.Scan(state.dest()...); err != nil {
		return GroupJoinRequest{}, err
	}
	return state.build(), nil
}

func scanGroupJoinRequestWithCounts(row scanner) (GroupJoinRequest, int, int, error) {
	state := groupJoinRequestScanState{}
	dest := state.dest()
	dest = append(dest, &state.total, &state.unread)
	if err := row.Scan(dest...); err != nil {
		return GroupJoinRequest{}, 0, 0, err
	}
	return state.build(), state.total, state.unread, nil
}

type groupJoinRequestScanState struct {
	request GroupJoinRequest

	requestID pgtype.UUID

	groupID            pgtype.UUID
	groupOwnerID       pgtype.UUID
	groupAvatarFileID  pgtype.UUID
	groupAvatarVersion int
	groupIntroduction  sql.NullString
	groupAnnouncement  sql.NullString

	actorID           pgtype.UUID
	actorGenderCustom sql.NullString
	actorAvatarFileID pgtype.UUID
	actorSignature    sql.NullString
	actorRegion       sql.NullString
	actorLastSeenAt   sql.NullTime
	actorDeletedAt    sql.NullTime

	operatorID           pgtype.UUID
	operatorPublicID     sql.NullString
	operatorEmail        sql.NullString
	operatorPasswordHash sql.NullString
	operatorNick         sql.NullString
	operatorGender       sql.NullString
	operatorGenderCustom sql.NullString
	operatorAvatarFileID pgtype.UUID
	operatorStatus       sql.NullString
	operatorSignature    sql.NullString
	operatorRegion       sql.NullString
	operatorLastSeenAt   sql.NullTime
	operatorTokenVersion sql.NullInt32
	operatorVersion      sql.NullInt32
	operatorCreatedAt    sql.NullTime
	operatorUpdatedAt    sql.NullTime
	operatorDeletedAt    sql.NullTime

	message   sql.NullString
	handledAt sql.NullTime
	total     int
	unread    int
}

func (s *groupJoinRequestScanState) dest() []any {
	return []any{
		&s.requestID,
		&s.groupID,
		&s.request.Group.PublicID,
		&s.request.Group.Name,
		&s.groupOwnerID,
		&s.groupAvatarFileID,
		&s.groupAvatarVersion,
		&s.groupIntroduction,
		&s.groupAnnouncement,
		&s.request.Group.Version,
		&s.request.Group.CreatedAt,
		&s.request.Group.UpdatedAt,
		&s.actorID,
		&s.request.ActorUser.PublicID,
		&s.request.ActorUser.Email,
		&s.request.ActorUser.PasswordHash,
		&s.request.ActorUser.Nick,
		&s.request.ActorUser.Gender,
		&s.actorGenderCustom,
		&s.actorAvatarFileID,
		&s.request.ActorUser.Status,
		&s.actorSignature,
		&s.actorRegion,
		&s.actorLastSeenAt,
		&s.request.ActorUser.TokenVersion,
		&s.request.ActorUser.Version,
		&s.request.ActorUser.CreatedAt,
		&s.request.ActorUser.UpdatedAt,
		&s.actorDeletedAt,
		&s.operatorID,
		&s.operatorPublicID,
		&s.operatorEmail,
		&s.operatorPasswordHash,
		&s.operatorNick,
		&s.operatorGender,
		&s.operatorGenderCustom,
		&s.operatorAvatarFileID,
		&s.operatorStatus,
		&s.operatorSignature,
		&s.operatorRegion,
		&s.operatorLastSeenAt,
		&s.operatorTokenVersion,
		&s.operatorVersion,
		&s.operatorCreatedAt,
		&s.operatorUpdatedAt,
		&s.operatorDeletedAt,
		&s.message,
		&s.request.Status,
		&s.handledAt,
		&s.request.CreatedAt,
		&s.request.UpdatedAt,
	}
}

func (s *groupJoinRequestScanState) build() GroupJoinRequest {
	request := s.request
	request.ID = uuidString(s.requestID)
	request.Group.ID = uuidString(s.groupID)
	request.Group.OwnerID = uuidString(s.groupOwnerID)
	request.Group.AvatarFileID = uuidStringPtr(s.groupAvatarFileID)
	request.Group.AvatarVersion = s.groupAvatarVersion
	if s.groupIntroduction.Valid {
		request.Group.Introduction = &s.groupIntroduction.String
	}
	if s.groupAnnouncement.Valid {
		request.Group.Announcement = &s.groupAnnouncement.String
	}
	request.ActorUser.ID = uuidString(s.actorID)
	if s.actorGenderCustom.Valid {
		request.ActorUser.GenderCustom = &s.actorGenderCustom.String
	}
	request.ActorUser.AvatarFileID = uuidStringPtr(s.actorAvatarFileID)
	if s.actorSignature.Valid {
		request.ActorUser.Signature = &s.actorSignature.String
	}
	if s.actorRegion.Valid {
		request.ActorUser.Region = &s.actorRegion.String
	}
	if s.actorLastSeenAt.Valid {
		request.ActorUser.LastSeenAt = &s.actorLastSeenAt.Time
	}
	if s.actorDeletedAt.Valid {
		request.ActorUser.DeletedAt = &s.actorDeletedAt.Time
	}

	if s.operatorID.Valid {
		operator := User{
			ID:           uuidString(s.operatorID),
			PublicID:     s.operatorPublicID.String,
			Email:        s.operatorEmail.String,
			PasswordHash: s.operatorPasswordHash.String,
			Nick:         s.operatorNick.String,
			Gender:       s.operatorGender.String,
			AvatarFileID: uuidStringPtr(s.operatorAvatarFileID),
			Status:       s.operatorStatus.String,
			TokenVersion: int(s.operatorTokenVersion.Int32),
			Version:      int(s.operatorVersion.Int32),
		}
		if s.operatorGenderCustom.Valid {
			operator.GenderCustom = &s.operatorGenderCustom.String
		}
		if s.operatorSignature.Valid {
			operator.Signature = &s.operatorSignature.String
		}
		if s.operatorRegion.Valid {
			operator.Region = &s.operatorRegion.String
		}
		if s.operatorLastSeenAt.Valid {
			operator.LastSeenAt = &s.operatorLastSeenAt.Time
		}
		if s.operatorCreatedAt.Valid {
			operator.CreatedAt = s.operatorCreatedAt.Time
		}
		if s.operatorUpdatedAt.Valid {
			operator.UpdatedAt = s.operatorUpdatedAt.Time
		}
		if s.operatorDeletedAt.Valid {
			operator.DeletedAt = &s.operatorDeletedAt.Time
		}
		request.OperatorUser = &operator
	}

	if s.message.Valid {
		request.Message = &s.message.String
	}
	if s.handledAt.Valid {
		request.HandledAt = &s.handledAt.Time
	}
	return request
}
