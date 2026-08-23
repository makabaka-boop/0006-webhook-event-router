package sqlite

import (
	"context"

	"webhook-event-router/internal/domain"
)

// AttemptStore 提供投递尝试记录的持久化。
type AttemptStore struct {
	db *DB
}

// NewAttemptStore 构造投递尝试存储。
func NewAttemptStore(db *DB) *AttemptStore { return &AttemptStore{db: db} }

// CreateDeliveryAttempt 写入一次投递尝试。
func (a *AttemptStore) CreateDeliveryAttempt(ctx context.Context, att *domain.DeliveryAttempt) error {
	ctx = persistenceContext(ctx)
	res, err := a.db.Conn().ExecContext(ctx,
		"INSERT INTO delivery_attempts(delivery_id, status, request_body, response_status, response_body, error, started_at, finished_at) VALUES(?,?,?,?,?,?,?,?)",
		att.DeliveryID, att.Status, att.RequestBody, att.ResponseStatus, att.ResponseBody, att.Error, now(), now(),
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	att.ID = id
	return nil
}

// ListByDelivery 返回某条投递的全部尝试记录（按时间升序，可回溯每次重试）。
func (a *AttemptStore) ListByDelivery(ctx context.Context, deliveryID int64) ([]*domain.DeliveryAttempt, error) {
	rows, err := a.db.Conn().QueryContext(ctx,
		"SELECT id,delivery_id,status,request_body,response_status,response_body,error,started_at,finished_at FROM delivery_attempts WHERE delivery_id=? ORDER BY id", deliveryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.DeliveryAttempt
	for rows.Next() {
		var att domain.DeliveryAttempt
		var sat, fat string
		if err := rows.Scan(&att.ID, &att.DeliveryID, &att.Status, &att.RequestBody, &att.ResponseStatus, &att.ResponseBody, &att.Error, &sat, &fat); err != nil {
			return nil, err
		}
		att.StartedAt = parseTime(sat)
		att.FinishedAt = parseTime(fat)
		out = append(out, &att)
	}
	return out, rows.Err()
}
