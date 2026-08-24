package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/store"
)

// Store 是 Store 接口的 SQLite 实现。
type Store struct {
	db            *DB
	targetScratch []int64
}

// New 构造 Store。
func New(db *DB) *Store { return &Store{db: db} }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// logChange 在事务内写入变更日志。
func (s *Store) logChange(t *sql.Tx, entityType string, entityID int64, action, before, after, actor string) error {
	_, err := t.Exec(
		"INSERT INTO change_logs(entity_type, entity_id, action, before, after, actor, created_at) VALUES(?,?,?,?,?,?,?)",
		entityType, entityID, action, before, after, actor, now(),
	)
	return err
}

// --- Source ---

func (s *Store) CreateSource(ctx context.Context, src *domain.Source) error {
	return tx(ctx, s.db.Conn(), func(t *sql.Tx) error {
		ts := now()
		res, err := t.Exec(
			"INSERT INTO sources(name, secret, enabled, allowed_event_types, created_at, updated_at) VALUES(?,?,?,?,?,?)",
			src.Name, src.Secret, boolInt(src.Enabled), mustJSON(src.AllowedTypes), ts, ts,
		)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		src.ID = id
		src.CreatedAt = time.Now().UTC()
		src.UpdatedAt = src.CreatedAt
		return s.logChange(t, "source", id, "create", "", mustJSON(src), "")
	})
}

func (s *Store) ListSources(ctx context.Context) ([]*domain.Source, error) {
	rows, err := s.db.Conn().QueryContext(ctx, "SELECT id,name,secret,enabled,allowed_event_types,created_at,updated_at FROM sources ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Source
	for rows.Next() {
		var src domain.Source
		var ats, cats, uats string
		if err := rows.Scan(&src.ID, &src.Name, &src.Secret, &src.Enabled, &ats, &cats, &uats); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(ats), &src.AllowedTypes)
		src.CreatedAt = parseTime(cats)
		src.UpdatedAt = parseTime(uats)
		out = append(out, &src)
	}
	return out, rows.Err()
}

func (s *Store) GetSource(ctx context.Context, id int64) (*domain.Source, error) {
	row := s.db.Conn().QueryRowContext(ctx, "SELECT id,name,secret,enabled,allowed_event_types,created_at,updated_at FROM sources WHERE id=?", id)
	var src domain.Source
	var ats, cats, uats string
	if err := row.Scan(&src.ID, &src.Name, &src.Secret, &src.Enabled, &ats, &cats, &uats); err != nil {
		if err == sql.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	json.Unmarshal([]byte(ats), &src.AllowedTypes)
	src.CreatedAt = parseTime(cats)
	src.UpdatedAt = parseTime(uats)
	return &src, nil
}

func (s *Store) UpdateSource(ctx context.Context, src *domain.Source) error {
	return tx(ctx, s.db.Conn(), func(t *sql.Tx) error {
		var before string
		existing, err := s.GetSource(ctx, src.ID)
		if err == nil {
			before = mustJSON(existing)
		}
		ts := now()
		_, err = t.Exec(
			"UPDATE sources SET name=?, secret=?, enabled=?, allowed_event_types=?, updated_at=? WHERE id=?",
			src.Name, src.Secret, boolInt(src.Enabled), mustJSON(src.AllowedTypes), ts, src.ID,
		)
		if err != nil {
			return err
		}
		src.UpdatedAt = time.Now().UTC()
		return s.logChange(t, "source", src.ID, "update", before, mustJSON(src), "")
	})
}

func (s *Store) DeleteSource(ctx context.Context, id int64) error {
	return tx(ctx, s.db.Conn(), func(t *sql.Tx) error {
		var before string
		existing, err := s.GetSource(ctx, id)
		if err == nil {
			before = mustJSON(existing)
		}
		if _, err := t.Exec("DELETE FROM sources WHERE id=?", id); err != nil {
			return err
		}
		return s.logChange(t, "source", id, "delete", before, "", "")
	})
}
