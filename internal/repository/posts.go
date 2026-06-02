package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const postSelectColumns = `
	id,
	author_id,
	title,
	content,
	cover_media_id,
	like_count,
	comment_count,
	content_created_at,
	visibility,
	version,
	created_at,
	updated_at,
	deleted_at`

const postSelectColumnsP = `
	p.id,
	p.author_id,
	p.title,
	p.content,
	p.cover_media_id,
	p.like_count,
	p.comment_count,
	p.content_created_at,
	p.visibility,
	p.version,
	p.created_at,
	p.updated_at,
	p.deleted_at`

const postMediaSelectColumns = `
	id,
	post_id,
	file_id,
	media_type,
	sort_order,
	width,
	height,
	duration_ms,
	cover_file_id,
	playback_json,
	live_status,
	metadata_json,
	created_at,
	updated_at`

const postCommentSelectColumns = `
	pc.id,
	pc.post_id,
	pc.author_id,
	pc.content,
	pc.like_count,
	pc.reply_count,
	pc.created_at,
	pc.updated_at,
	pc.deleted_at`

const postReplySelectColumns = `
	pr.id,
	pr.comment_id,
	pr.post_id,
	pr.author_id,
	pr.target_user_id,
	pr.target_reply_id,
	pr.content,
	pr.like_count,
	pr.created_at,
	pr.updated_at,
	pr.deleted_at`

type PostRepository struct {
	db DBTX
}

type Post struct {
	ID               string
	AuthorID         string
	Title            *string
	Content          string
	CoverMediaID     *string
	LikeCount        int
	CommentCount     int
	ContentCreatedAt *time.Time
	Visibility       string
	Version          int
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

type PostMedia struct {
	ID          string
	PostID      string
	FileID      string
	MediaType   string
	SortOrder   int
	Width       *int
	Height      *int
	DurationMS  *int
	CoverFileID *string
	Playback    json.RawMessage
	LiveStatus  *string
	Metadata    json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type PostViewerState struct {
	IsLiked          bool
	IsFollowedAuthor bool
}

type PostEntry struct {
	Post   Post
	Author User
	Media  []PostMedia
	Viewer PostViewerState
}

type CreatePostParams struct {
	AuthorID         string
	Title            *string
	Content          string
	ContentCreatedAt *time.Time
	Visibility       string
}

type UpdatePostParams struct {
	PostID     string
	AuthorID   string
	Title      *string
	Content    string
	Visibility string
}

type CreatePostMediaParams struct {
	FileID      string
	MediaType   string
	SortOrder   int
	Width       *int
	Height      *int
	DurationMS  *int
	CoverFileID *string
	Playback    json.RawMessage
	LiveStatus  *string
	Metadata    json.RawMessage
}

type PostComment struct {
	ID         string
	PostID     string
	AuthorID   string
	Content    string
	LikeCount  int
	ReplyCount int
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time
	Author     User
	Viewer     CommentViewerState
	Replies    []PostReply
}

type PostReply struct {
	ID            string
	CommentID     string
	PostID        string
	AuthorID      string
	TargetUserID  *string
	TargetReplyID *string
	Content       string
	LikeCount     int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
	Author        User
	TargetUser    *User
	Viewer        CommentViewerState
}

type CommentViewerState struct {
	IsLiked bool
}

func NewPostRepository(db DBTX) *PostRepository {
	return &PostRepository{db: db}
}

func (r *PostRepository) ListPosts(ctx context.Context, viewerUserID string, followOnly bool, limit int, offset int) ([]PostEntry, int, error) {
	rows, err := r.db.Query(ctx, postEntrySelectSQL()+`
		WHERE p.deleted_at IS NULL
		  AND (p.visibility = 'public' OR p.author_id = $1::uuid)
		  AND (
		    $2 = false
		    OR p.author_id = $1::uuid
		    OR EXISTS (
		      SELECT 1 FROM follows f
		      WHERE f.follower_user_id = $1::uuid
		        AND f.target_user_id = p.author_id
		    )
		  )
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $3 OFFSET $4`,
		normalizeText(viewerUserID),
		followOnly,
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, translateReadError(err)
	}
	defer rows.Close()

	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (postEntryWithTotal, error) {
		return scanPostEntry(row)
	})
	if err != nil {
		return nil, 0, translateReadError(err)
	}
	if err := r.loadPostEntryMedia(ctx, entries); err != nil {
		return nil, 0, err
	}

	total := 0
	if len(entries) > 0 {
		total = entries[0].totalCount
	}
	return stripPostEntryTotals(entries), total, nil
}

func (r *PostRepository) GetPost(ctx context.Context, viewerUserID string, postID string) (PostEntry, error) {
	entry, err := scanPostEntry(r.db.QueryRow(ctx, postEntrySelectSQL()+`
		WHERE p.id = $2::uuid
		  AND p.deleted_at IS NULL
		  AND (p.visibility = 'public' OR p.author_id = $1::uuid)`,
		normalizeText(viewerUserID),
		normalizeText(postID),
	))
	if err != nil {
		return PostEntry{}, translateReadError(err)
	}
	entries := []postEntryWithTotal{entry}
	if err := r.loadPostEntryMedia(ctx, entries); err != nil {
		return PostEntry{}, err
	}
	return entries[0].PostEntry, nil
}

func (r *PostRepository) CreatePost(ctx context.Context, params CreatePostParams) (Post, error) {
	visibility := normalizeText(params.Visibility)
	if visibility == "" {
		visibility = "public"
	}
	post, err := scanPost(r.db.QueryRow(ctx, `
		INSERT INTO posts (author_id, title, content, content_created_at, visibility)
		VALUES ($1::uuid, $2, $3, $4, $5)
		RETURNING `+postSelectColumns,
		normalizeText(params.AuthorID),
		normalizeTextPtr(params.Title),
		normalizeText(params.Content),
		params.ContentCreatedAt,
		visibility,
	))
	return post, translateWriteError(err)
}

func (r *PostRepository) UpdatePost(ctx context.Context, params UpdatePostParams) (Post, error) {
	post, err := scanPost(r.db.QueryRow(ctx, `
		UPDATE posts p
		SET title = $3,
		    content = $4,
		    visibility = $5,
		    version = version + 1,
		    updated_at = now()
		WHERE p.id = $1::uuid
		  AND p.author_id = $2::uuid
		  AND p.deleted_at IS NULL
		RETURNING `+postSelectColumns,
		normalizeText(params.PostID),
		normalizeText(params.AuthorID),
		normalizeTextPtr(params.Title),
		normalizeText(params.Content),
		normalizeText(params.Visibility),
	))
	return post, translateWriteError(err)
}

func (r *PostRepository) UpdatePostMediaUsageVisibility(ctx context.Context, postID string, visibility string) error {
	usageVisibility := normalizeText(visibility)
	if usageVisibility == "" {
		usageVisibility = "private"
	}
	_, err := r.db.Exec(ctx, `
		UPDATE file_usages
		SET visibility = $2,
		    updated_at = now()
		WHERE usage_type IN ('post_media', 'post_media_cover')
		  AND business_id = $1::uuid
		  AND deleted_at IS NULL`,
		normalizeText(postID),
		usageVisibility,
	)
	return translateWriteError(err)
}

func (r *PostRepository) ReplacePostMedia(ctx context.Context, postID string, ownerUserID string, visibility string, media []CreatePostMediaParams) ([]PostMedia, error) {
	usageVisibility := normalizeText(visibility)
	if usageVisibility == "" {
		usageVisibility = "private"
	}
	if _, err := r.db.Exec(ctx, `
		UPDATE file_usages
		SET deleted_at = now(), updated_at = now()
		WHERE usage_type IN ('post_media', 'post_media_cover')
		  AND business_id = $1::uuid
		  AND deleted_at IS NULL`,
		normalizeText(postID),
	); err != nil {
		return nil, translateWriteError(err)
	}
	if _, err := r.db.Exec(ctx, `DELETE FROM post_media WHERE post_id = $1::uuid`, normalizeText(postID)); err != nil {
		return nil, translateWriteError(err)
	}
	if _, err := r.db.Exec(ctx, `UPDATE posts SET cover_media_id = NULL WHERE id = $1::uuid`, normalizeText(postID)); err != nil {
		return nil, translateWriteError(err)
	}

	created := make([]PostMedia, 0, len(media))
	for _, item := range media {
		row := r.db.QueryRow(ctx, `
			INSERT INTO post_media (
				post_id,
				file_id,
				media_type,
				sort_order,
				width,
				height,
				duration_ms,
				cover_file_id,
				playback_json,
				live_status,
				metadata_json
			)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8::uuid, $9, $10, $11)
			RETURNING `+postMediaSelectColumns,
			normalizeText(postID),
			normalizeText(item.FileID),
			normalizeText(item.MediaType),
			item.SortOrder,
			item.Width,
			item.Height,
			item.DurationMS,
			normalizeTextPtr(item.CoverFileID),
			jsonOrDefault(item.Playback),
			normalizeTextPtr(item.LiveStatus),
			jsonOrDefault(item.Metadata),
		)
		createdMedia, err := scanPostMediaScanner(row)
		if err != nil {
			return nil, translateWriteError(err)
		}
		created = append(created, createdMedia)

		if _, err := r.db.Exec(ctx, `
			INSERT INTO file_usages (file_id, usage_type, business_id, owner_user_id, visibility)
			VALUES ($1::uuid, 'post_media', $2::uuid, $3::uuid, $4)`,
			normalizeText(item.FileID),
			normalizeText(postID),
			normalizeText(ownerUserID),
			usageVisibility,
		); err != nil {
			return nil, translateWriteError(err)
		}
		if item.CoverFileID != nil && normalizeText(*item.CoverFileID) != "" {
			if _, err := r.db.Exec(ctx, `
				INSERT INTO file_usages (file_id, usage_type, business_id, owner_user_id, visibility)
				VALUES ($1::uuid, 'post_media_cover', $2::uuid, $3::uuid, $4)`,
				normalizeText(*item.CoverFileID),
				normalizeText(postID),
				normalizeText(ownerUserID),
				usageVisibility,
			); err != nil {
				return nil, translateWriteError(err)
			}
		}
	}

	if len(created) > 0 {
		if _, err := r.db.Exec(ctx, `
			UPDATE posts
			SET cover_media_id = $2::uuid,
			    version = version + 1,
			    updated_at = now()
			WHERE id = $1::uuid`,
			normalizeText(postID),
			normalizeText(created[0].ID),
		); err != nil {
			return nil, translateWriteError(err)
		}
	}

	return created, nil
}

func (r *PostRepository) DeletePost(ctx context.Context, postID string, actorUserID string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE posts
		SET deleted_at = now(),
		    version = version + 1,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND author_id = $2::uuid
		  AND deleted_at IS NULL`,
		normalizeText(postID),
		normalizeText(actorUserID),
	)
	if err != nil {
		return translateWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, err = r.db.Exec(ctx, `
		UPDATE file_usages
		SET deleted_at = now(), updated_at = now()
		WHERE usage_type IN ('post_media', 'post_media_cover')
		  AND business_id = $1::uuid
		  AND deleted_at IS NULL`,
		normalizeText(postID),
	)
	return translateWriteError(err)
}

func (r *PostRepository) SetPostLike(ctx context.Context, postID string, userID string, liked bool) (int, bool, error) {
	if liked {
		if _, err := r.db.Exec(ctx, `
			INSERT INTO post_likes (post_id, user_id)
			SELECT p.id, $2::uuid
			FROM posts p
			WHERE p.id = $1::uuid
			  AND p.deleted_at IS NULL
			  AND (p.visibility = 'public' OR p.author_id = $2::uuid)
			ON CONFLICT (post_id, user_id) DO NOTHING`,
			normalizeText(postID),
			normalizeText(userID),
		); err != nil {
			return 0, false, translateWriteError(err)
		}
	} else {
		if _, err := r.db.Exec(ctx, `
			DELETE FROM post_likes
			WHERE post_id = $1::uuid
			  AND user_id = $2::uuid`,
			normalizeText(postID),
			normalizeText(userID),
		); err != nil {
			return 0, false, translateWriteError(err)
		}
	}
	return r.refreshPostLikeCount(ctx, postID, userID)
}

func (r *PostRepository) SetFollow(ctx context.Context, followerUserID string, targetUserID string, followed bool) (bool, error) {
	if followed {
		if _, err := r.db.Exec(ctx, `
			INSERT INTO follows (follower_user_id, target_user_id)
			VALUES ($1::uuid, $2::uuid)
			ON CONFLICT (follower_user_id, target_user_id) DO NOTHING`,
			normalizeText(followerUserID),
			normalizeText(targetUserID),
		); err != nil {
			return false, translateWriteError(err)
		}
	} else {
		if _, err := r.db.Exec(ctx, `
			DELETE FROM follows
			WHERE follower_user_id = $1::uuid
			  AND target_user_id = $2::uuid`,
			normalizeText(followerUserID),
			normalizeText(targetUserID),
		); err != nil {
			return false, translateWriteError(err)
		}
	}

	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM follows
			WHERE follower_user_id = $1::uuid
			  AND target_user_id = $2::uuid
		)`,
		normalizeText(followerUserID),
		normalizeText(targetUserID),
	).Scan(&exists)
	return exists, translateReadError(err)
}

func (r *PostRepository) CreateComment(ctx context.Context, postID string, authorID string, content string) (PostComment, error) {
	comment, err := scanPostCommentWithAuthor(r.db.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO post_comments (post_id, author_id, content)
			SELECT p.id, $2::uuid, $3
			FROM posts p
			WHERE p.id = $1::uuid
			  AND p.deleted_at IS NULL
			  AND (p.visibility = 'public' OR p.author_id = $2::uuid)
			RETURNING *
		),
		counted AS (
			UPDATE posts p
			SET comment_count = comment_count + 1,
			    updated_at = now()
			FROM inserted i
			WHERE p.id = i.post_id
			RETURNING p.id
		)
		SELECT `+postCommentSelectColumns+`, `+userSelectColumnsU+`, false AS viewer_liked
		FROM inserted pc
		JOIN users u ON u.id = pc.author_id`,
		normalizeText(postID),
		normalizeText(authorID),
		normalizeText(content),
	))
	return comment, translateWriteError(err)
}

func (r *PostRepository) ListComments(ctx context.Context, viewerUserID string, postID string, limit int, offset int) ([]PostComment, int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+postCommentSelectColumns+`, `+userSelectColumnsU+`,
		       EXISTS (
		         SELECT 1 FROM post_comment_likes pcl
		         WHERE pcl.comment_id = pc.id
		           AND pcl.user_id = $1::uuid
		       ) AS viewer_liked,
		       (count(*) OVER())::int AS total_count
		FROM post_comments pc
		JOIN posts p ON p.id = pc.post_id
		JOIN users u ON u.id = pc.author_id
		WHERE pc.post_id = $2::uuid
		  AND pc.deleted_at IS NULL
		  AND p.deleted_at IS NULL
		  AND (p.visibility = 'public' OR p.author_id = $1::uuid)
		ORDER BY pc.created_at DESC, pc.id DESC
		LIMIT $3 OFFSET $4`,
		normalizeText(viewerUserID),
		normalizeText(postID),
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, translateReadError(err)
	}
	defer rows.Close()

	comments, err := pgx.CollectRows(rows, scanPostCommentWithTotal)
	if err != nil {
		return nil, 0, translateReadError(err)
	}
	if err := r.loadRepliesForComments(ctx, viewerUserID, comments); err != nil {
		return nil, 0, err
	}

	total := 0
	if len(comments) > 0 {
		total = comments[0].totalCount
	}
	return stripPostCommentTotals(comments), total, nil
}

func (r *PostRepository) GetCommentPostID(ctx context.Context, commentID string, viewerUserID string) (string, error) {
	var postID pgtype.UUID
	err := r.db.QueryRow(ctx, `
		SELECT pc.post_id
		FROM post_comments pc
		JOIN posts p ON p.id = pc.post_id
		WHERE pc.id = $1::uuid
		  AND pc.deleted_at IS NULL
		  AND p.deleted_at IS NULL
		  AND (p.visibility = 'public' OR p.author_id = $2::uuid OR pc.author_id = $2::uuid)`,
		normalizeText(commentID),
		normalizeText(viewerUserID),
	).Scan(&postID)
	return uuidString(postID), translateReadError(err)
}

func (r *PostRepository) GetReplyPostID(ctx context.Context, replyID string, viewerUserID string) (string, error) {
	var postID pgtype.UUID
	err := r.db.QueryRow(ctx, `
		SELECT pr.post_id
		FROM post_comment_replies pr
		JOIN posts p ON p.id = pr.post_id
		WHERE pr.id = $1::uuid
		  AND pr.deleted_at IS NULL
		  AND p.deleted_at IS NULL
		  AND (p.visibility = 'public' OR p.author_id = $2::uuid OR pr.author_id = $2::uuid)`,
		normalizeText(replyID),
		normalizeText(viewerUserID),
	).Scan(&postID)
	return uuidString(postID), translateReadError(err)
}

func (r *PostRepository) SetCommentLike(ctx context.Context, commentID string, userID string, liked bool) (int, bool, error) {
	if liked {
		if _, err := r.db.Exec(ctx, `
			INSERT INTO post_comment_likes (comment_id, user_id)
			SELECT pc.id, $2::uuid
			FROM post_comments pc
			JOIN posts p ON p.id = pc.post_id
			WHERE pc.id = $1::uuid
			  AND pc.deleted_at IS NULL
			  AND p.deleted_at IS NULL
			  AND (p.visibility = 'public' OR p.author_id = $2::uuid)
			ON CONFLICT (comment_id, user_id) DO NOTHING`,
			normalizeText(commentID),
			normalizeText(userID),
		); err != nil {
			return 0, false, translateWriteError(err)
		}
	} else {
		if _, err := r.db.Exec(ctx, `
			DELETE FROM post_comment_likes
			WHERE comment_id = $1::uuid
			  AND user_id = $2::uuid
			  AND EXISTS (
			    SELECT 1
			    FROM post_comments pc
			    JOIN posts p ON p.id = pc.post_id
			    WHERE pc.id = $1::uuid
			      AND pc.deleted_at IS NULL
			      AND p.deleted_at IS NULL
			      AND (p.visibility = 'public' OR p.author_id = $2::uuid)
			  )`,
			normalizeText(commentID),
			normalizeText(userID),
		); err != nil {
			return 0, false, translateWriteError(err)
		}
	}
	return r.refreshCommentLikeCount(ctx, commentID, userID)
}

func (r *PostRepository) CreateReply(ctx context.Context, commentID string, authorID string, targetUserID *string, targetReplyID *string, content string) (PostReply, error) {
	reply, err := scanPostReplyWithAuthors(r.db.QueryRow(ctx, `
		WITH target_comment AS (
			SELECT pc.id, pc.post_id
			FROM post_comments pc
			JOIN posts p ON p.id = pc.post_id AND p.deleted_at IS NULL
			WHERE pc.id = $1::uuid
			  AND pc.deleted_at IS NULL
			  AND (p.visibility = 'public' OR p.author_id = $2::uuid)
		),
		inserted AS (
			INSERT INTO post_comment_replies (
				comment_id,
				post_id,
				author_id,
				target_user_id,
				target_reply_id,
				content
			)
			SELECT tc.id, tc.post_id, $2::uuid, $3::uuid, $4::uuid, $5
			FROM target_comment tc
			RETURNING *
		),
		counted AS (
			UPDATE post_comments pc
			SET reply_count = reply_count + 1,
			    updated_at = now()
			FROM inserted i
			WHERE pc.id = i.comment_id
			RETURNING pc.id
		)
		SELECT `+postReplySelectColumns+`, `+userSelectColumnsU+`, false AS viewer_liked,
		       tu.id,
		       tu.public_id,
		       tu.email,
		       tu.password_hash,
		       tu.nick,
		       tu.gender,
		       tu.gender_custom,
		       tu.avatar_file_id,
		       tu.status,
		       tu.signature,
		       tu.region,
		       tu.last_seen_at,
		       tu.token_version,
		       tu.version,
		       tu.created_at,
		       tu.updated_at,
		       tu.deleted_at
		FROM inserted pr
		JOIN users u ON u.id = pr.author_id
		LEFT JOIN users tu ON tu.id = pr.target_user_id`,
		normalizeText(commentID),
		normalizeText(authorID),
		normalizeTextPtr(targetUserID),
		normalizeTextPtr(targetReplyID),
		normalizeText(content),
	))
	return reply, translateWriteError(err)
}

func (r *PostRepository) SetReplyLike(ctx context.Context, replyID string, userID string, liked bool) (int, bool, error) {
	if liked {
		if _, err := r.db.Exec(ctx, `
			INSERT INTO post_reply_likes (reply_id, user_id)
			SELECT pr.id, $2::uuid
			FROM post_comment_replies pr
			JOIN posts p ON p.id = pr.post_id
			WHERE pr.id = $1::uuid
			  AND pr.deleted_at IS NULL
			  AND p.deleted_at IS NULL
			  AND (p.visibility = 'public' OR p.author_id = $2::uuid)
			ON CONFLICT (reply_id, user_id) DO NOTHING`,
			normalizeText(replyID),
			normalizeText(userID),
		); err != nil {
			return 0, false, translateWriteError(err)
		}
	} else {
		if _, err := r.db.Exec(ctx, `
			DELETE FROM post_reply_likes
			WHERE reply_id = $1::uuid
			  AND user_id = $2::uuid
			  AND EXISTS (
			    SELECT 1
			    FROM post_comment_replies pr
			    JOIN posts p ON p.id = pr.post_id
			    WHERE pr.id = $1::uuid
			      AND pr.deleted_at IS NULL
			      AND p.deleted_at IS NULL
			      AND (p.visibility = 'public' OR p.author_id = $2::uuid)
			  )`,
			normalizeText(replyID),
			normalizeText(userID),
		); err != nil {
			return 0, false, translateWriteError(err)
		}
	}
	return r.refreshReplyLikeCount(ctx, replyID, userID)
}

func (r *PostRepository) DeleteComment(ctx context.Context, commentID string, actorUserID string) error {
	tag, err := r.db.Exec(ctx, `
		WITH target AS (
			SELECT pc.id, pc.post_id
			FROM post_comments pc
			JOIN posts p ON p.id = pc.post_id
			WHERE pc.id = $1::uuid
			  AND pc.deleted_at IS NULL
			  AND (pc.author_id = $2::uuid OR p.author_id = $2::uuid)
		),
		updated_comment AS (
			UPDATE post_comments pc
			SET deleted_at = now(), updated_at = now()
			FROM target t
			WHERE pc.id = t.id
			RETURNING pc.post_id
		)
		UPDATE posts p
		SET comment_count = GREATEST(comment_count - 1, 0),
		    updated_at = now()
		FROM updated_comment uc
		WHERE p.id = uc.post_id`,
		normalizeText(commentID),
		normalizeText(actorUserID),
	)
	if err != nil {
		return translateWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostRepository) DeleteReply(ctx context.Context, replyID string, actorUserID string) error {
	tag, err := r.db.Exec(ctx, `
		WITH target AS (
			SELECT pr.id, pr.comment_id
			FROM post_comment_replies pr
			JOIN posts p ON p.id = pr.post_id
			WHERE pr.id = $1::uuid
			  AND pr.deleted_at IS NULL
			  AND (pr.author_id = $2::uuid OR p.author_id = $2::uuid)
		),
		updated_reply AS (
			UPDATE post_comment_replies pr
			SET deleted_at = now(), updated_at = now()
			FROM target t
			WHERE pr.id = t.id
			RETURNING pr.comment_id
		)
		UPDATE post_comments pc
		SET reply_count = GREATEST(reply_count - 1, 0),
		    updated_at = now()
		FROM updated_reply ur
		WHERE pc.id = ur.comment_id`,
		normalizeText(replyID),
		normalizeText(actorUserID),
	)
	if err != nil {
		return translateWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostRepository) refreshPostLikeCount(ctx context.Context, postID string, userID string) (int, bool, error) {
	var count int
	var isLiked bool
	err := r.db.QueryRow(ctx, `
		WITH counted AS (
			SELECT p.id,
			       (SELECT count(*)::int FROM post_likes pl WHERE pl.post_id = p.id) AS like_count,
			       EXISTS (
			         SELECT 1 FROM post_likes pl
			         WHERE pl.post_id = p.id
			           AND pl.user_id = $2::uuid
			       ) AS is_liked
			FROM posts p
			WHERE p.id = $1::uuid
			  AND p.deleted_at IS NULL
			  AND (p.visibility = 'public' OR p.author_id = $2::uuid)
		),
		updated AS (
			UPDATE posts p
			SET like_count = c.like_count,
			    updated_at = now()
			FROM counted c
			WHERE p.id = c.id
			RETURNING c.like_count, c.is_liked
		)
		SELECT like_count, is_liked FROM updated`,
		normalizeText(postID),
		normalizeText(userID),
	).Scan(&count, &isLiked)
	return count, isLiked, translateWriteError(err)
}

func (r *PostRepository) refreshCommentLikeCount(ctx context.Context, commentID string, userID string) (int, bool, error) {
	var count int
	var isLiked bool
	err := r.db.QueryRow(ctx, `
		WITH counted AS (
			SELECT pc.id,
			       (SELECT count(*)::int FROM post_comment_likes pcl WHERE pcl.comment_id = pc.id) AS like_count,
			       EXISTS (
			         SELECT 1 FROM post_comment_likes pcl
			         WHERE pcl.comment_id = pc.id
			           AND pcl.user_id = $2::uuid
			       ) AS is_liked
			FROM post_comments pc
			JOIN posts p ON p.id = pc.post_id
			WHERE pc.id = $1::uuid
			  AND pc.deleted_at IS NULL
			  AND p.deleted_at IS NULL
			  AND (p.visibility = 'public' OR p.author_id = $2::uuid)
		),
		updated AS (
			UPDATE post_comments pc
			SET like_count = c.like_count,
			    updated_at = now()
			FROM counted c
			WHERE pc.id = c.id
			RETURNING c.like_count, c.is_liked
		)
		SELECT like_count, is_liked FROM updated`,
		normalizeText(commentID),
		normalizeText(userID),
	).Scan(&count, &isLiked)
	return count, isLiked, translateWriteError(err)
}

func (r *PostRepository) refreshReplyLikeCount(ctx context.Context, replyID string, userID string) (int, bool, error) {
	var count int
	var isLiked bool
	err := r.db.QueryRow(ctx, `
		WITH counted AS (
			SELECT pr.id,
			       (SELECT count(*)::int FROM post_reply_likes prl WHERE prl.reply_id = pr.id) AS like_count,
			       EXISTS (
			         SELECT 1 FROM post_reply_likes prl
			         WHERE prl.reply_id = pr.id
			           AND prl.user_id = $2::uuid
			       ) AS is_liked
			FROM post_comment_replies pr
			JOIN posts p ON p.id = pr.post_id
			WHERE pr.id = $1::uuid
			  AND pr.deleted_at IS NULL
			  AND p.deleted_at IS NULL
			  AND (p.visibility = 'public' OR p.author_id = $2::uuid)
		),
		updated AS (
			UPDATE post_comment_replies pr
			SET like_count = c.like_count,
			    updated_at = now()
			FROM counted c
			WHERE pr.id = c.id
			RETURNING c.like_count, c.is_liked
		)
		SELECT like_count, is_liked FROM updated`,
		normalizeText(replyID),
		normalizeText(userID),
	).Scan(&count, &isLiked)
	return count, isLiked, translateWriteError(err)
}

func postEntrySelectSQL() string {
	return `
		SELECT ` + postSelectColumnsP + `, ` + userSelectColumnsU + `,
		       EXISTS (
		         SELECT 1 FROM post_likes pl
		         WHERE pl.post_id = p.id
		           AND pl.user_id = $1::uuid
		       ) AS viewer_liked,
		       EXISTS (
		         SELECT 1 FROM follows f
		         WHERE f.follower_user_id = $1::uuid
		           AND f.target_user_id = p.author_id
		       ) AS viewer_followed_author,
		       (count(*) OVER())::int AS total_count
		FROM posts p
		JOIN users u ON u.id = p.author_id`
}

type postEntryWithTotal struct {
	PostEntry
	totalCount int
}

func scanPostEntry(row scanner) (postEntryWithTotal, error) {
	var entry postEntryWithTotal
	var (
		postID               pgtype.UUID
		authorID             pgtype.UUID
		coverMediaID         pgtype.UUID
		postTitle            sql.NullString
		postContentCreatedAt sql.NullTime
		postDeletedAt        sql.NullTime
		userID               pgtype.UUID
		genderCustom         sql.NullString
		avatarFileID         pgtype.UUID
		signature            sql.NullString
		region               sql.NullString
		lastSeenAt           sql.NullTime
		userDeletedAt        sql.NullTime
	)
	var viewerLiked bool
	var viewerFollowed bool
	if err := row.Scan(
		&postID,
		&authorID,
		&postTitle,
		&entry.Post.Content,
		&coverMediaID,
		&entry.Post.LikeCount,
		&entry.Post.CommentCount,
		&postContentCreatedAt,
		&entry.Post.Visibility,
		&entry.Post.Version,
		&entry.Post.CreatedAt,
		&entry.Post.UpdatedAt,
		&postDeletedAt,
		&userID,
		&entry.Author.PublicID,
		&entry.Author.Email,
		&entry.Author.PasswordHash,
		&entry.Author.Nick,
		&entry.Author.Gender,
		&genderCustom,
		&avatarFileID,
		&entry.Author.Status,
		&signature,
		&region,
		&lastSeenAt,
		&entry.Author.TokenVersion,
		&entry.Author.Version,
		&entry.Author.CreatedAt,
		&entry.Author.UpdatedAt,
		&userDeletedAt,
		&viewerLiked,
		&viewerFollowed,
		&entry.totalCount,
	); err != nil {
		return postEntryWithTotal{}, err
	}
	entry.Post.ID = uuidString(postID)
	entry.Post.AuthorID = uuidString(authorID)
	if postTitle.Valid {
		entry.Post.Title = &postTitle.String
	}
	entry.Post.CoverMediaID = uuidStringPtr(coverMediaID)
	if postContentCreatedAt.Valid {
		entry.Post.ContentCreatedAt = &postContentCreatedAt.Time
	}
	if postDeletedAt.Valid {
		entry.Post.DeletedAt = &postDeletedAt.Time
	}
	entry.Author.ID = uuidString(userID)
	entry.Author.AvatarFileID = uuidStringPtr(avatarFileID)
	if genderCustom.Valid {
		entry.Author.GenderCustom = &genderCustom.String
	}
	if signature.Valid {
		entry.Author.Signature = &signature.String
	}
	if region.Valid {
		entry.Author.Region = &region.String
	}
	if lastSeenAt.Valid {
		entry.Author.LastSeenAt = &lastSeenAt.Time
	}
	if userDeletedAt.Valid {
		entry.Author.DeletedAt = &userDeletedAt.Time
	}
	entry.Viewer = PostViewerState{IsLiked: viewerLiked, IsFollowedAuthor: viewerFollowed}
	return entry, nil
}

func stripPostEntryTotals(entries []postEntryWithTotal) []PostEntry {
	result := make([]PostEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.PostEntry)
	}
	return result
}

func (r *PostRepository) loadPostEntryMedia(ctx context.Context, entries []postEntryWithTotal) error {
	for i := range entries {
		media, err := r.ListPostMedia(ctx, entries[i].Post.ID)
		if err != nil {
			return err
		}
		entries[i].Media = media
	}
	return nil
}

func (r *PostRepository) ListPostMedia(ctx context.Context, postID string) ([]PostMedia, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+postMediaSelectColumns+`
		FROM post_media
		WHERE post_id = $1::uuid
		ORDER BY sort_order ASC, created_at ASC`,
		normalizeText(postID),
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()
	media, err := pgx.CollectRows(rows, scanPostMedia)
	return media, translateReadError(err)
}

func scanPost(row scanner) (Post, error) {
	var post Post
	var (
		id               pgtype.UUID
		authorID         pgtype.UUID
		title            sql.NullString
		coverMediaID     pgtype.UUID
		contentCreatedAt sql.NullTime
		deletedAt        sql.NullTime
	)
	err := row.Scan(
		&id,
		&authorID,
		&title,
		&post.Content,
		&coverMediaID,
		&post.LikeCount,
		&post.CommentCount,
		&contentCreatedAt,
		&post.Visibility,
		&post.Version,
		&post.CreatedAt,
		&post.UpdatedAt,
		&deletedAt,
	)
	post.ID = uuidString(id)
	post.AuthorID = uuidString(authorID)
	if title.Valid {
		post.Title = &title.String
	}
	post.CoverMediaID = uuidStringPtr(coverMediaID)
	if contentCreatedAt.Valid {
		post.ContentCreatedAt = &contentCreatedAt.Time
	}
	if deletedAt.Valid {
		post.DeletedAt = &deletedAt.Time
	}
	return post, err
}

func scanPostMedia(row pgx.CollectableRow) (PostMedia, error) {
	return scanPostMediaScanner(row)
}

func scanPostMediaScanner(row scanner) (PostMedia, error) {
	var media PostMedia
	var (
		id          pgtype.UUID
		postID      pgtype.UUID
		fileID      pgtype.UUID
		width       sql.NullInt32
		height      sql.NullInt32
		durationMS  sql.NullInt32
		coverFileID pgtype.UUID
		liveStatus  sql.NullString
	)
	err := row.Scan(
		&id,
		&postID,
		&fileID,
		&media.MediaType,
		&media.SortOrder,
		&width,
		&height,
		&durationMS,
		&coverFileID,
		&media.Playback,
		&liveStatus,
		&media.Metadata,
		&media.CreatedAt,
		&media.UpdatedAt,
	)
	media.ID = uuidString(id)
	media.PostID = uuidString(postID)
	media.FileID = uuidString(fileID)
	if width.Valid {
		value := int(width.Int32)
		media.Width = &value
	}
	if height.Valid {
		value := int(height.Int32)
		media.Height = &value
	}
	if durationMS.Valid {
		value := int(durationMS.Int32)
		media.DurationMS = &value
	}
	media.CoverFileID = uuidStringPtr(coverFileID)
	if liveStatus.Valid {
		media.LiveStatus = &liveStatus.String
	}
	media.Playback = jsonOrDefault(media.Playback)
	media.Metadata = jsonOrDefault(media.Metadata)
	return media, err
}

type postCommentWithTotal struct {
	PostComment
	totalCount int
}

func scanPostCommentWithAuthor(row scanner) (PostComment, error) {
	var comment PostComment
	var (
		commentID    pgtype.UUID
		postID       pgtype.UUID
		authorID     pgtype.UUID
		deletedAt    sql.NullTime
		userID       pgtype.UUID
		genderCustom sql.NullString
		avatarFileID pgtype.UUID
		signature    sql.NullString
		region       sql.NullString
		lastSeenAt   sql.NullTime
		userDeleted  sql.NullTime
	)
	var viewerLiked bool
	err := row.Scan(
		&commentID,
		&postID,
		&authorID,
		&comment.Content,
		&comment.LikeCount,
		&comment.ReplyCount,
		&comment.CreatedAt,
		&comment.UpdatedAt,
		&deletedAt,
		&userID,
		&comment.Author.PublicID,
		&comment.Author.Email,
		&comment.Author.PasswordHash,
		&comment.Author.Nick,
		&comment.Author.Gender,
		&genderCustom,
		&avatarFileID,
		&comment.Author.Status,
		&signature,
		&region,
		&lastSeenAt,
		&comment.Author.TokenVersion,
		&comment.Author.Version,
		&comment.Author.CreatedAt,
		&comment.Author.UpdatedAt,
		&userDeleted,
		&viewerLiked,
	)
	comment.ID = uuidString(commentID)
	comment.PostID = uuidString(postID)
	comment.AuthorID = uuidString(authorID)
	if deletedAt.Valid {
		comment.DeletedAt = &deletedAt.Time
	}
	comment.Author.ID = uuidString(userID)
	comment.Author.AvatarFileID = uuidStringPtr(avatarFileID)
	if genderCustom.Valid {
		comment.Author.GenderCustom = &genderCustom.String
	}
	if signature.Valid {
		comment.Author.Signature = &signature.String
	}
	if region.Valid {
		comment.Author.Region = &region.String
	}
	if lastSeenAt.Valid {
		comment.Author.LastSeenAt = &lastSeenAt.Time
	}
	if userDeleted.Valid {
		comment.Author.DeletedAt = &userDeleted.Time
	}
	comment.Viewer = CommentViewerState{IsLiked: viewerLiked}
	return comment, err
}

func scanPostCommentWithTotal(row pgx.CollectableRow) (postCommentWithTotal, error) {
	var comment postCommentWithTotal
	var (
		commentID    pgtype.UUID
		postID       pgtype.UUID
		authorID     pgtype.UUID
		deletedAt    sql.NullTime
		userID       pgtype.UUID
		genderCustom sql.NullString
		avatarFileID pgtype.UUID
		signature    sql.NullString
		region       sql.NullString
		lastSeenAt   sql.NullTime
		userDeleted  sql.NullTime
	)
	var viewerLiked bool
	err := row.Scan(
		&commentID,
		&postID,
		&authorID,
		&comment.Content,
		&comment.LikeCount,
		&comment.ReplyCount,
		&comment.CreatedAt,
		&comment.UpdatedAt,
		&deletedAt,
		&userID,
		&comment.Author.PublicID,
		&comment.Author.Email,
		&comment.Author.PasswordHash,
		&comment.Author.Nick,
		&comment.Author.Gender,
		&genderCustom,
		&avatarFileID,
		&comment.Author.Status,
		&signature,
		&region,
		&lastSeenAt,
		&comment.Author.TokenVersion,
		&comment.Author.Version,
		&comment.Author.CreatedAt,
		&comment.Author.UpdatedAt,
		&userDeleted,
		&viewerLiked,
		&comment.totalCount,
	)
	comment.ID = uuidString(commentID)
	comment.PostID = uuidString(postID)
	comment.AuthorID = uuidString(authorID)
	if deletedAt.Valid {
		comment.DeletedAt = &deletedAt.Time
	}
	comment.Author.ID = uuidString(userID)
	comment.Author.AvatarFileID = uuidStringPtr(avatarFileID)
	if genderCustom.Valid {
		comment.Author.GenderCustom = &genderCustom.String
	}
	if signature.Valid {
		comment.Author.Signature = &signature.String
	}
	if region.Valid {
		comment.Author.Region = &region.String
	}
	if lastSeenAt.Valid {
		comment.Author.LastSeenAt = &lastSeenAt.Time
	}
	if userDeleted.Valid {
		comment.Author.DeletedAt = &userDeleted.Time
	}
	comment.Viewer = CommentViewerState{IsLiked: viewerLiked}
	return comment, err
}

func stripPostCommentTotals(comments []postCommentWithTotal) []PostComment {
	result := make([]PostComment, 0, len(comments))
	for _, comment := range comments {
		result = append(result, comment.PostComment)
	}
	return result
}

func (r *PostRepository) loadRepliesForComments(ctx context.Context, viewerUserID string, comments []postCommentWithTotal) error {
	for i := range comments {
		replies, err := r.listRepliesForComment(ctx, viewerUserID, comments[i].ID)
		if err != nil {
			return err
		}
		comments[i].Replies = replies
	}
	return nil
}

func (r *PostRepository) listRepliesForComment(ctx context.Context, viewerUserID string, commentID string) ([]PostReply, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+postReplySelectColumns+`, `+userSelectColumnsU+`,
		       EXISTS (
		         SELECT 1 FROM post_reply_likes prl
		         WHERE prl.reply_id = pr.id
		           AND prl.user_id = $1::uuid
		       ) AS viewer_liked,
		       tu.id,
		       tu.public_id,
		       tu.email,
		       tu.password_hash,
		       tu.nick,
		       tu.gender,
		       tu.gender_custom,
		       tu.avatar_file_id,
		       tu.status,
		       tu.signature,
		       tu.region,
		       tu.last_seen_at,
		       tu.token_version,
		       tu.version,
		       tu.created_at,
		       tu.updated_at,
		       tu.deleted_at
		FROM post_comment_replies pr
		JOIN users u ON u.id = pr.author_id
		LEFT JOIN users tu ON tu.id = pr.target_user_id
		WHERE pr.comment_id = $2::uuid
		  AND pr.deleted_at IS NULL
		ORDER BY pr.created_at ASC, pr.id ASC`,
		normalizeText(viewerUserID),
		normalizeText(commentID),
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()
	replies, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PostReply, error) {
		return scanPostReplyWithAuthors(row)
	})
	return replies, translateReadError(err)
}

func scanPostReplyWithAuthors(row scanner) (PostReply, error) {
	var reply PostReply
	var viewerLiked bool
	var (
		replyID         pgtype.UUID
		commentID       pgtype.UUID
		postID          pgtype.UUID
		authorID        pgtype.UUID
		targetUserID    pgtype.UUID
		targetReplyID   pgtype.UUID
		replyDeletedAt  sql.NullTime
		userID          pgtype.UUID
		genderCustom    sql.NullString
		avatarFileID    pgtype.UUID
		signature       sql.NullString
		region          sql.NullString
		lastSeenAt      sql.NullTime
		authorDeletedAt sql.NullTime
	)
	var targetUser User
	var targetID pgtype.UUID
	var targetPublicID sql.NullString
	var targetEmail sql.NullString
	var targetPasswordHash sql.NullString
	var targetNick sql.NullString
	var targetGender sql.NullString
	var targetGenderCustom sql.NullString
	var targetAvatarFileID pgtype.UUID
	var targetStatus sql.NullString
	var targetSignature sql.NullString
	var targetRegion sql.NullString
	var targetLastSeenAt sql.NullTime
	var targetTokenVersion sql.NullInt32
	var targetVersion sql.NullInt32
	var targetCreatedAt sql.NullTime
	var targetUpdatedAt sql.NullTime
	var targetDeletedAt sql.NullTime

	err := row.Scan(
		&replyID,
		&commentID,
		&postID,
		&authorID,
		&targetUserID,
		&targetReplyID,
		&reply.Content,
		&reply.LikeCount,
		&reply.CreatedAt,
		&reply.UpdatedAt,
		&replyDeletedAt,
		&userID,
		&reply.Author.PublicID,
		&reply.Author.Email,
		&reply.Author.PasswordHash,
		&reply.Author.Nick,
		&reply.Author.Gender,
		&genderCustom,
		&avatarFileID,
		&reply.Author.Status,
		&signature,
		&region,
		&lastSeenAt,
		&reply.Author.TokenVersion,
		&reply.Author.Version,
		&reply.Author.CreatedAt,
		&reply.Author.UpdatedAt,
		&authorDeletedAt,
		&viewerLiked,
		&targetID,
		&targetPublicID,
		&targetEmail,
		&targetPasswordHash,
		&targetNick,
		&targetGender,
		&targetGenderCustom,
		&targetAvatarFileID,
		&targetStatus,
		&targetSignature,
		&targetRegion,
		&targetLastSeenAt,
		&targetTokenVersion,
		&targetVersion,
		&targetCreatedAt,
		&targetUpdatedAt,
		&targetDeletedAt,
	)
	if err != nil {
		return PostReply{}, err
	}
	reply.ID = uuidString(replyID)
	reply.CommentID = uuidString(commentID)
	reply.PostID = uuidString(postID)
	reply.AuthorID = uuidString(authorID)
	reply.TargetUserID = uuidStringPtr(targetUserID)
	reply.TargetReplyID = uuidStringPtr(targetReplyID)
	if replyDeletedAt.Valid {
		reply.DeletedAt = &replyDeletedAt.Time
	}
	reply.Author.ID = uuidString(userID)
	reply.Author.AvatarFileID = uuidStringPtr(avatarFileID)
	if genderCustom.Valid {
		reply.Author.GenderCustom = &genderCustom.String
	}
	if signature.Valid {
		reply.Author.Signature = &signature.String
	}
	if region.Valid {
		reply.Author.Region = &region.String
	}
	if lastSeenAt.Valid {
		reply.Author.LastSeenAt = &lastSeenAt.Time
	}
	if authorDeletedAt.Valid {
		reply.Author.DeletedAt = &authorDeletedAt.Time
	}
	reply.Viewer = CommentViewerState{IsLiked: viewerLiked}
	if targetID.Valid {
		targetUser.ID = targetID.String()
		targetUser.PublicID = targetPublicID.String
		targetUser.Email = targetEmail.String
		targetUser.PasswordHash = targetPasswordHash.String
		targetUser.Nick = targetNick.String
		targetUser.Gender = targetGender.String
		if targetGenderCustom.Valid {
			value := targetGenderCustom.String
			targetUser.GenderCustom = &value
		}
		if targetAvatarFileID.Valid {
			value := targetAvatarFileID.String()
			targetUser.AvatarFileID = &value
		}
		targetUser.Status = targetStatus.String
		if targetSignature.Valid {
			value := targetSignature.String
			targetUser.Signature = &value
		}
		if targetRegion.Valid {
			value := targetRegion.String
			targetUser.Region = &value
		}
		if targetLastSeenAt.Valid {
			targetUser.LastSeenAt = &targetLastSeenAt.Time
		}
		targetUser.TokenVersion = int(targetTokenVersion.Int32)
		targetUser.Version = int(targetVersion.Int32)
		targetUser.CreatedAt = targetCreatedAt.Time
		targetUser.UpdatedAt = targetUpdatedAt.Time
		if targetDeletedAt.Valid {
			targetUser.DeletedAt = &targetDeletedAt.Time
		}
		reply.TargetUser = &targetUser
	}
	return reply, nil
}
