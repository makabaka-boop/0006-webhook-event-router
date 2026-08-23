package sqlite

import (
	"context"

	"webhook-event-router/internal/domain"
)

// ListChangeLogs 按实体类型与实体 ID（可选）分页查询变更日志。
func (s *Store) ListChangeLogs(ctx context.Context, entityType string, entityID int64, page, limit int) ([]*domain.ChangeLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit
	q := "SELECT id,entity_type,entity_id,action,before,after,actor,created_at FROM change_logs WHERE 1=1"
	var args []any
	if entityType != "" {
		q += " AND entity_type=?"
		args = append(args, entityType)
	}
	if entityID > 0 {
		q += " AND entity_id=?"
		args = append(args, entityID)
	}
	q += " ORDER BY id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Conn().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.ChangeLog
	for rows.Next() {
		var cl domain.ChangeLog
		var cats string
		if err := rows.Scan(&cl.ID, &cl.EntityType, &cl.EntityID, &cl.Action, &cl.Before, &cl.After, &cl.Actor, &cats); err != nil {
			return nil, err
		}
		cl.CreatedAt = parseTime(cats)
		out = append(out, &cl)
	}
	return out, rows.Err()
}
