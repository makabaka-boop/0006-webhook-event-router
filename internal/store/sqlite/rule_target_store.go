package sqlite

import (
	"context"
	"database/sql"
)

// CreateRuleTargets 在事务内写入一条规则的多个目标关联。
func (s *Store) CreateRuleTargets(ctx context.Context, ruleID int64, targetIDs []int64) error {
	return tx(ctx, s.db.Conn(), func(t *sql.Tx) error {
		for _, tid := range targetIDs {
			if _, err := t.ExecContext(ctx,
				"INSERT OR IGNORE INTO rule_targets(rule_id, target_id) VALUES(?,?)", ruleID, tid); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListRuleTargets 返回规则关联的目标 ID 列表（升序，保持稳定）。
func (s *Store) ListRuleTargets(ctx context.Context, ruleID int64) ([]int64, error) {
	rows, err := s.db.Conn().QueryContext(ctx,
		"SELECT target_id FROM rule_targets WHERE rule_id=? ORDER BY target_id", ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var tid int64
		if err := rows.Scan(&tid); err != nil {
			return nil, err
		}
		out = append(out, tid)
	}
	return out, rows.Err()
}

// DeleteRuleTargets 删除规则的全部目标关联。
func (s *Store) DeleteRuleTargets(ctx context.Context, ruleID int64) error {
	_, err := s.db.Conn().ExecContext(ctx, "DELETE FROM rule_targets WHERE rule_id=?", ruleID)
	return err
}
