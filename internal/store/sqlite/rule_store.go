package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/store"
)

func (s *Store) CreateRule(ctx context.Context, r *domain.Rule) error {
	return tx(ctx, s.db.Conn(), func(tx *sql.Tx) error {
		ts := now()
		res, err := tx.Exec(
			"INSERT INTO rules(name, source_id, event_type, condition, target_id, priority, enabled, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?)",
			r.Name, nullableInt(r.SourceID), r.EventType, mustJSON(r.Condition), r.TargetID, r.Priority, boolInt(r.Enabled), ts, ts,
		)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		r.ID = id
		r.CreatedAt = time.Now().UTC()
		r.UpdatedAt = r.CreatedAt
		if err := s.persistTargets(ctx, tx, r); err != nil {
			return err
		}
		return s.logChange(tx, "rule", id, "create", "", mustJSON(r), "")
	})
}

func (s *Store) ListRules(ctx context.Context) ([]*domain.Rule, error) {
	return s.queryRules(ctx, "SELECT id,name,source_id,event_type,condition,target_id,priority,enabled,created_at,updated_at FROM rules ORDER BY priority DESC, id")
}

func (s *Store) ListEnabledRules(ctx context.Context) ([]*domain.Rule, error) {
	return s.queryRules(ctx, "SELECT id,name,source_id,event_type,condition,target_id,priority,enabled,created_at,updated_at FROM rules WHERE enabled=1 ORDER BY priority DESC, id")
}

func (s *Store) queryRules(ctx context.Context, q string) ([]*domain.Rule, error) {
	rows, err := s.db.Conn().QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	var out []*domain.Rule
	for rows.Next() {
		var r domain.Rule
		var sid sql.NullInt64
		var cond, cats, uats string
		if err := rows.Scan(&r.ID, &r.Name, &sid, &r.EventType, &cond, &r.TargetID, &r.Priority, &r.Enabled, &cats, &uats); err != nil {
			rows.Close()
			return nil, err
		}
		if sid.Valid {
			v := sid.Int64
			r.SourceID = &v
		}
		json.Unmarshal([]byte(cond), &r.Condition)
		r.CreatedAt = parseTime(cats)
		r.UpdatedAt = parseTime(uats)
		out = append(out, &r)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, r := range out {
		if err := s.loadTargets(ctx, r); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) GetRule(ctx context.Context, id int64) (*domain.Rule, error) {
	row := s.db.Conn().QueryRowContext(ctx, "SELECT id,name,source_id,event_type,condition,target_id,priority,enabled,created_at,updated_at FROM rules WHERE id=?", id)
	var r domain.Rule
	var sid sql.NullInt64
	var cond, cats, uats string
	if err := row.Scan(&r.ID, &r.Name, &sid, &r.EventType, &cond, &r.TargetID, &r.Priority, &r.Enabled, &cats, &uats); err != nil {
		if err == sql.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if sid.Valid {
		v := sid.Int64
		r.SourceID = &v
	}
	json.Unmarshal([]byte(cond), &r.Condition)
	r.CreatedAt = parseTime(cats)
	r.UpdatedAt = parseTime(uats)
	if err := s.loadTargets(ctx, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) UpdateRule(ctx context.Context, r *domain.Rule) error {
	return tx(ctx, s.db.Conn(), func(tx *sql.Tx) error {
		var before string
		if existing, err := s.GetRule(ctx, r.ID); err == nil {
			before = mustJSON(existing)
		}
		ts := now()
		if _, err := tx.Exec(
			"UPDATE rules SET name=?, source_id=?, event_type=?, condition=?, target_id=?, priority=?, enabled=?, updated_at=? WHERE id=?",
			r.Name, nullableInt(r.SourceID), r.EventType, mustJSON(r.Condition), r.TargetID, r.Priority, boolInt(r.Enabled), ts, r.ID,
		); err != nil {
			return err
		}
		// 重建目标关联以支持多目标规则的变更。
		if _, err := tx.ExecContext(ctx, "DELETE FROM rule_targets WHERE rule_id=?", r.ID); err != nil {
			return err
		}
		if err := s.persistTargets(ctx, tx, r); err != nil {
			return err
		}
		r.UpdatedAt = time.Now().UTC()
		return s.logChange(tx, "rule", r.ID, "update", before, mustJSON(r), "")
	})
}

func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	return tx(ctx, s.db.Conn(), func(tx *sql.Tx) error {
		var before string
		if existing, err := s.GetRule(ctx, id); err == nil {
			before = mustJSON(existing)
		}
		if _, err := tx.Exec("DELETE FROM rule_targets WHERE rule_id=?", id); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM rules WHERE id=?", id); err != nil {
			return err
		}
		return s.logChange(tx, "rule", id, "delete", before, "", "")
	})
}

func nullableInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// persistTargets 将规则的目标列表写入 rule_targets 关联表。
func (s *Store) persistTargets(ctx context.Context, tx *sql.Tx, r *domain.Rule) error {
	ids := r.TargetIDs
	if len(ids) == 0 && r.TargetID != 0 {
		ids = []int64{r.TargetID}
	}
	for _, tid := range ids {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO rule_targets(rule_id, target_id) VALUES(?,?)", r.ID, tid); err != nil {
			return err
		}
	}
	return nil
}

// loadTargets 读取规则的目标关联并填充 TargetIDs。
func (s *Store) loadTargets(ctx context.Context, r *domain.Rule) error {
	ids, err := s.ListRuleTargets(ctx, r.ID)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		r.TargetIDs = ids
	}
	return nil
}
