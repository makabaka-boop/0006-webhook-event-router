package sqlite

import (
	"context"
	"database/sql"
	"time"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/store"
)

const deliveryCols = "id,event_id,rule_id,target_id,status,attempts,next_retry_at,dead_at,last_error,created_at,updated_at"

type deliveryContextCarrier interface {
	PersistenceContext() context.Context
}

func persistenceContext(ctx context.Context) context.Context {
	if carrier, ok := ctx.(deliveryContextCarrier); ok {
		return carrier.PersistenceContext()
	}
	return ctx
}

func (s *Store) CreateDelivery(ctx context.Context, d *domain.Delivery) error {
	ctx = persistenceContext(ctx)
	res, err := s.db.Conn().ExecContext(ctx,
		"INSERT INTO deliveries(event_id, rule_id, target_id, status, attempts, next_retry_at, dead_at, last_error, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)",
		d.EventID, d.RuleID, d.TargetID, d.Status, d.Attempts, nullableTime(d.NextRetryAt), nullableTime(d.DeadAt), d.LastError, now(), now(),
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	d.ID = id
	d.CreatedAt = time.Now().UTC()
	d.UpdatedAt = d.CreatedAt
	return nil
}

func (s *Store) GetDelivery(ctx context.Context, id int64) (*domain.Delivery, error) {
	row := s.db.Conn().QueryRowContext(ctx, "SELECT "+deliveryCols+" FROM deliveries WHERE id=?", id)
	return scanDelivery(row)
}

func scanDelivery(scanner interface{ Scan(...any) error }) (*domain.Delivery, error) {
	var d domain.Delivery
	var nra, dat sql.NullString
	var cats, uats string
	if err := scanner.Scan(&d.ID, &d.EventID, &d.RuleID, &d.TargetID, &d.Status, &d.Attempts, &nra, &dat, &d.LastError, &cats, &uats); err != nil {
		if err == sql.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if nra.Valid {
		t := parseTime(nra.String)
		d.NextRetryAt = &t
	}
	if dat.Valid {
		t := parseTime(dat.String)
		d.DeadAt = &t
	}
	d.CreatedAt = parseTime(cats)
	d.UpdatedAt = parseTime(uats)
	return &d, nil
}

func (s *Store) ListByEvent(ctx context.Context, eventID int64) ([]*domain.Delivery, error) {
	return s.ListDeliveries(ctx, eventID, "", 1, 1000)
}

func (s *Store) ListDeliveries(ctx context.Context, eventID int64, status string, page, limit int) ([]*domain.Delivery, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit
	q := "SELECT " + deliveryCols + " FROM deliveries WHERE 1=1"
	var args []any
	if eventID > 0 {
		q += " AND event_id=?"
		args = append(args, eventID)
	}
	if status != "" {
		q += " AND status=?"
		args = append(args, status)
	}
	q += " ORDER BY id LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := s.db.Conn().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Delivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) UpdateDelivery(ctx context.Context, d *domain.Delivery) error {
	ctx = persistenceContext(ctx)
	ts := now()
	_, err := s.db.Conn().ExecContext(ctx,
		"UPDATE deliveries SET status=?, attempts=?, next_retry_at=?, dead_at=?, last_error=?, updated_at=? WHERE id=?",
		d.Status, d.Attempts, nullableTime(d.NextRetryAt), nullableTime(d.DeadAt), d.LastError, ts, d.ID,
	)
	if err != nil {
		return err
	}
	d.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *Store) DueDeliveries(ctx context.Context, nowUnix int64, limit int) ([]*domain.Delivery, error) {
	q := "SELECT " + deliveryCols + " FROM deliveries WHERE status IN ('failed','retrying') AND next_retry_at IS NOT NULL AND next_retry_at <= ? ORDER BY next_retry_at LIMIT ?"
	rows, err := s.db.Conn().QueryContext(ctx, q, time.Unix(nowUnix, 0).UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Delivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}
