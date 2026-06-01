package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type GroupRepository struct {
	db DBTX
}

type Group struct {
	ID           string
	Name         string
	OwnerID      string
	AvatarFileID *string
	Introduction *string
	Announcement *string
	Version      int
	CreatedAt    time.Time
	UpdatedAt    time.Time
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

func NewGroupRepository(db DBTX) *GroupRepository {
	return &GroupRepository{db: db}
}

func (r *GroupRepository) ListForUser(ctx context.Context, userID string, keyword string, categoryID string, limit int, offset int) ([]GroupListEntry, int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			g.id,
			g.name,
			g.owner_id,
			g.avatar_file_id,
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
		  AND ($2 = '' OR g.name ILIKE '%' || $2 || '%' OR gus.remark ILIKE '%' || $2 || '%')
		  AND ($3 = '' OR gus.list_group_id = NULLIF($3, '')::uuid)
		ORDER BY COALESCE(gus.remark, g.name) ASC, g.created_at DESC
		LIMIT $4 OFFSET $5`,
		normalizeText(userID),
		normalizeText(keyword),
		normalizeText(categoryID),
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
			g.name,
			g.owner_id,
			g.avatar_file_id,
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
		&entry.Group.Name,
		&ownerID,
		&avatarFileID,
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
