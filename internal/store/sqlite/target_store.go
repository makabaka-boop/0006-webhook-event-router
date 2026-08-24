package sqlite

import (
	"context"
	"database/sql"
	"time"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/store"
)

func (s *Store) CreateTarget(ctx context.Context, t *domain.Target) error {
	return tx(ctx, s.db.Conn(), func(tx *sql.Tx) error {
		ts := now()
		res, err := tx.Exec(
			"INSERT INTO targets(name, url, enabled, created_at, updated_at) VALUES(?,?,?,?,?)",
			t.Name, t.URL, boolInt(t.Enabled), ts, ts,
		)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		t.ID = id
		t.CreatedAt = time.Now().UTC()
		t.UpdatedAt = t.CreatedAt
		return s.logChange(tx, "target", id, "create", "", mustJSON(t), "")
	})
}

func (s *Store) ListTargets(ctx context.Context) ([]*domain.Target, error) {
	rows, err := s.db.Conn().QueryContext(ctx, "SELECT id,name,url,enabled,created_at,updated_at FROM targets ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Target
	for rows.Next() {
		var t domain.Target
		var cats, uats string
		if err := rows.Scan(&t.ID, &t.Name, &t.URL, &t.Enabled, &cats, &uats); err != nil {
			return nil, err
		}
		t.CreatedAt = parseTime(cats)
		t.UpdatedAt = parseTime(uats)
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (s *Store) GetTarget(ctx context.Context, id int64) (*domain.Target, error) {
	row := s.db.Conn().QueryRowContext(ctx, "SELECT id,name,url,enabled,created_at,updated_at FROM targets WHERE id=?", id)
	var t domain.Target
	var cats, uats string
	if err := row.Scan(&t.ID, &t.Name, &t.URL, &t.Enabled, &cats, &uats); err != nil {
		if err == sql.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	t.CreatedAt = parseTime(cats)
	t.UpdatedAt = parseTime(uats)
	return &t, nil
}

func (s *Store) UpdateTarget(ctx context.Context, t *domain.Target) error {
	return tx(ctx, s.db.Conn(), func(tx *sql.Tx) error {
		var before string
		if existing, err := s.GetTarget(ctx, t.ID); err == nil {
			before = mustJSON(existing)
		}
		ts := now()
		if _, err := tx.Exec("UPDATE targets SET name=?, url=?, enabled=?, updated_at=? WHERE id=?",
			t.Name, t.URL, boolInt(t.Enabled), ts, t.ID); err != nil {
			return err
		}
		t.UpdatedAt = time.Now().UTC()
		return s.logChange(tx, "target", t.ID, "update", before, mustJSON(t), "")
	})
}

func (s *Store) DeleteTarget(ctx context.Context, id int64) error {
	return tx(ctx, s.db.Conn(), func(tx *sql.Tx) error {
		var before string
		if existing, err := s.GetTarget(ctx, id); err == nil {
			before = mustJSON(existing)
		}
		if _, err := tx.Exec("DELETE FROM targets WHERE id=?", id); err != nil {
			return err
		}
		return s.logChange(tx, "target", id, "delete", before, "", "")
	})
}
