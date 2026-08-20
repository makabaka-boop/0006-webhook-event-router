package sqlite

import (
	"context"
	"database/sql"
	"time"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/store"
)

func (s *Store) CreateEvent(ctx context.Context, e *domain.Event) error {
	ts := now()
	res, err := s.db.Conn().ExecContext(ctx,
		"INSERT INTO events(source_id, event_type, event_id, payload, status, reject_reason, received_at, created_at) VALUES(?,?,?,?,?,?,?,?)",
		e.SourceID, e.EventType, e.EventID, e.Payload, e.Status, e.RejectReason, ts, ts,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	e.ID = id
	e.ReceivedAt = time.Now().UTC()
	e.CreatedAt = e.ReceivedAt
	return nil
}

func (s *Store) GetEvent(ctx context.Context, id int64) (*domain.Event, error) {
	row := s.db.Conn().QueryRowContext(ctx, "SELECT id,source_id,event_type,event_id,payload,status,reject_reason,received_at,created_at FROM events WHERE id=?", id)
	var e domain.Event
	var rts, cats string
	if err := row.Scan(&e.ID, &e.SourceID, &e.EventType, &e.EventID, &e.Payload, &e.Status, &e.RejectReason, &rts, &cats); err != nil {
		if err == sql.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	e.ReceivedAt = parseTime(rts)
	e.CreatedAt = parseTime(cats)
	return &e, nil
}

func (s *Store) GetByKey(ctx context.Context, sourceID int64, eventID string) (*domain.Event, error) {
	row := s.db.Conn().QueryRowContext(ctx, "SELECT id,source_id,event_type,event_id,payload,status,reject_reason,received_at,created_at FROM events WHERE source_id=? AND event_id=?", sourceID, eventID)
	var e domain.Event
	var rts, cats string
	if err := row.Scan(&e.ID, &e.SourceID, &e.EventType, &e.EventID, &e.Payload, &e.Status, &e.RejectReason, &rts, &cats); err != nil {
		if err == sql.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	e.ReceivedAt = parseTime(rts)
	e.CreatedAt = parseTime(cats)
	return &e, nil
}

func (s *Store) UpdateStatus(ctx context.Context, id int64, status string, reason string) error {
	_, err := s.db.Conn().ExecContext(ctx, "UPDATE events SET status=?, reject_reason=? WHERE id=?", status, reason, id)
	return err
}
