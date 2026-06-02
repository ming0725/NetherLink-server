package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type RealtimeEventRepository struct {
	db DBTX
}

type RealtimeEvent struct {
	EventSeq  uint64
	EventID   string
	Type      string
	EmittedAt time.Time
	Payload   json.RawMessage
}

func NewRealtimeEventRepository(db DBTX) *RealtimeEventRepository {
	return &RealtimeEventRepository{db: db}
}

func (r *RealtimeEventRepository) Create(ctx context.Context, userIDs []string, eventType string, payload json.RawMessage) (RealtimeEvent, error) {
	event, err := scanRealtimeEvent(r.db.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO realtime_events (
				type,
				payload_json
			)
			VALUES ($1, $2)
			RETURNING event_seq, type, payload_json, emitted_at
		),
		recipients AS (
			INSERT INTO realtime_event_recipients (
				event_seq,
				user_id
			)
			SELECT inserted.event_seq, recipient.user_id::uuid
			FROM inserted
			CROSS JOIN unnest($3::text[]) AS recipient(user_id)
			WHERE recipient.user_id <> ''
			ON CONFLICT DO NOTHING
			RETURNING 1
		)
		SELECT event_seq, 'evt_' || event_seq::text, type, payload_json, emitted_at
		FROM inserted`,
		normalizeText(eventType),
		jsonOrDefault(payload),
		userIDs,
	))
	return event, translateWriteError(err)
}

func (r *RealtimeEventRepository) ListForUserAfter(ctx context.Context, userID string, afterSeq uint64, limit int) ([]RealtimeEvent, bool, error) {
	rows, err := r.db.Query(ctx, `
		SELECT e.event_seq, 'evt_' || e.event_seq::text, e.type, e.payload_json, e.emitted_at
		FROM realtime_events e
		JOIN realtime_event_recipients er
		  ON er.event_seq = e.event_seq
		 AND er.user_id = $1::uuid
		WHERE e.event_seq > $2
		ORDER BY e.event_seq ASC
		LIMIT $3`,
		normalizeText(userID),
		afterSeq,
		limit+1,
	)
	if err != nil {
		return nil, false, translateReadError(err)
	}
	defer rows.Close()

	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (RealtimeEvent, error) {
		return scanRealtimeEvent(row)
	})
	if err != nil {
		return nil, false, translateReadError(err)
	}

	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	return events, hasMore, nil
}

func (r *RealtimeEventRepository) LatestSeq(ctx context.Context) (uint64, error) {
	var latest int64
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(MAX(event_seq), 0)
		FROM realtime_events`,
	).Scan(&latest)
	return uint64(latest), translateReadError(err)
}

func (r *RealtimeEventRepository) DeleteBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}

	tag, err := r.db.Exec(ctx, `
		WITH expired AS (
			SELECT event_seq
			FROM realtime_events
			WHERE emitted_at < $1
			ORDER BY event_seq ASC
			LIMIT $2
		)
		DELETE FROM realtime_events e
		USING expired
		WHERE e.event_seq = expired.event_seq`,
		before,
		limit,
	)
	return tag.RowsAffected(), translateWriteError(err)
}

func scanRealtimeEvent(row scanner) (RealtimeEvent, error) {
	var event RealtimeEvent
	var seq int64
	err := row.Scan(
		&seq,
		&event.EventID,
		&event.Type,
		&event.Payload,
		&event.EmittedAt,
	)
	event.EventSeq = uint64(seq)
	return event, err
}
