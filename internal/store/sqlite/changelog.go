package sqlite

import (
	"context"

	"webhook-event-router/internal/domain"
)

func (s *Store) CreateChangeLog(ctx context.Context, cl *domain.ChangeLog) error {
	_, err := s.db.Conn().ExecContext(ctx,
		"INSERT INTO change_logs(entity_type, entity_id, action, before, after, actor, created_at) VALUES(?,?,?,?,?,?,?)",
		cl.EntityType, cl.EntityID, cl.Action, cl.Before, cl.After, cl.Actor, now(),
	)
	return err
}
