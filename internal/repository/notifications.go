package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const notificationSelectColumns = `
	id,
	user_id,
	type,
	source_id,
	payload_json,
	read_at,
	created_at,
	updated_at`

type NotificationRepository struct {
	db DBTX
}

type Notification struct {
	ID        string
	UserID    string
	Type      string
	SourceID  *string
	Payload   json.RawMessage
	ReadAt    *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewNotificationRepository(db DBTX) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) List(ctx context.Context, userID string, notificationType string, limit int, offset int) ([]Notification, int, int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+notificationSelectColumns+`,
		       (count(*) OVER())::int AS total_count,
		       (
		         SELECT count(*)::int
		         FROM notifications unread
		         WHERE unread.user_id = $1::uuid
		           AND unread.read_at IS NULL
		           AND ($2 = '' OR unread.type = $2)
		       ) AS unread_count
		FROM notifications
		WHERE user_id = $1::uuid
		  AND ($2 = '' OR type = $2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4`,
		normalizeText(userID),
		normalizeText(notificationType),
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, 0, translateReadError(err)
	}
	defer rows.Close()

	notifications := []Notification{}
	total := 0
	unread := 0
	for rows.Next() {
		notification, rowTotal, rowUnread, err := scanNotificationWithCounts(rows)
		if err != nil {
			return nil, 0, 0, err
		}
		total = rowTotal
		unread = rowUnread
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	return notifications, total, unread, nil
}

func (r *NotificationRepository) MarkRead(ctx context.Context, userID string, notificationID string) (Notification, error) {
	notification, err := scanNotification(r.db.QueryRow(ctx, `
		UPDATE notifications
		SET read_at = COALESCE(read_at, now()),
		    updated_at = now()
		WHERE id = $1::uuid
		  AND user_id = $2::uuid
		RETURNING `+notificationSelectColumns,
		normalizeText(notificationID),
		normalizeText(userID),
	))
	return notification, translateWriteError(err)
}

func (r *NotificationRepository) MarkAllRead(ctx context.Context, userID string, notificationType string) (int64, int, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE notifications
		SET read_at = COALESCE(read_at, now()),
		    updated_at = now()
		WHERE user_id = $1::uuid
		  AND read_at IS NULL
		  AND ($2 = '' OR type = $2)`,
		normalizeText(userID),
		normalizeText(notificationType),
	)
	if err != nil {
		return 0, 0, err
	}

	var unread int
	err = r.db.QueryRow(ctx, `
		SELECT count(*)::int
		FROM notifications
		WHERE user_id = $1::uuid
		  AND read_at IS NULL
		  AND ($2 = '' OR type = $2)`,
		normalizeText(userID),
		normalizeText(notificationType),
	).Scan(&unread)
	if err != nil {
		return 0, 0, translateReadError(err)
	}
	return tag.RowsAffected(), unread, nil
}

func scanNotification(row scanner) (Notification, error) {
	var (
		notification Notification
		id           pgtype.UUID
		userID       pgtype.UUID
		sourceID     pgtype.UUID
		readAt       sql.NullTime
	)

	err := row.Scan(
		&id,
		&userID,
		&notification.Type,
		&sourceID,
		&notification.Payload,
		&readAt,
		&notification.CreatedAt,
		&notification.UpdatedAt,
	)
	if err != nil {
		return Notification{}, err
	}

	notification.ID = uuidString(id)
	notification.UserID = uuidString(userID)
	notification.SourceID = uuidStringPtr(sourceID)
	notification.Payload = jsonOrDefault(notification.Payload)
	if readAt.Valid {
		notification.ReadAt = &readAt.Time
	}
	return notification, nil
}

func scanNotificationWithCounts(row scanner) (Notification, int, int, error) {
	var (
		notification Notification
		id           pgtype.UUID
		userID       pgtype.UUID
		sourceID     pgtype.UUID
		readAt       sql.NullTime
		total        int
		unread       int
	)

	err := row.Scan(
		&id,
		&userID,
		&notification.Type,
		&sourceID,
		&notification.Payload,
		&readAt,
		&notification.CreatedAt,
		&notification.UpdatedAt,
		&total,
		&unread,
	)
	if err != nil {
		return Notification{}, 0, 0, err
	}

	notification.ID = uuidString(id)
	notification.UserID = uuidString(userID)
	notification.SourceID = uuidStringPtr(sourceID)
	notification.Payload = jsonOrDefault(notification.Payload)
	if readAt.Valid {
		notification.ReadAt = &readAt.Time
	}
	return notification, total, unread, nil
}
