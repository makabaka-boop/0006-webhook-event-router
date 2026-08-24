package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB 封装 SQLite 连接，开启 WAL 与 foreign_keys。
type DB struct {
	conn *sql.DB
}

// Open 打开数据库并执行迁移。
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		// 目录不存在时由调用方负责，此处仅确保连接串正确。
		_ = dir
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, err
	}
	return db, nil
}

// Conn 返回底层连接。
func (d *DB) Conn() *sql.DB { return d.conn }

// Close 关闭连接。
func (d *DB) Close() error { return d.conn.Close() }

func (d *DB) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS sources (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  secret TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  allowed_event_types TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  event_id TEXT NOT NULL,
  payload TEXT NOT NULL,
  status TEXT NOT NULL,
  reject_reason TEXT NOT NULL DEFAULT '',
  received_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(source_id, event_id),
  FOREIGN KEY(source_id) REFERENCES sources(id)
);
CREATE TABLE IF NOT EXISTS targets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  url TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  source_id INTEGER,
  event_type TEXT NOT NULL DEFAULT '',
  condition TEXT NOT NULL DEFAULT '[]',
  target_id INTEGER NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(source_id) REFERENCES sources(id),
  FOREIGN KEY(target_id) REFERENCES targets(id)
);
CREATE TABLE IF NOT EXISTS deliveries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id INTEGER NOT NULL,
  rule_id INTEGER NOT NULL,
  target_id INTEGER NOT NULL,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_retry_at TEXT,
  dead_at TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(event_id) REFERENCES events(id),
  FOREIGN KEY(rule_id) REFERENCES rules(id),
  FOREIGN KEY(target_id) REFERENCES targets(id)
);
CREATE TABLE IF NOT EXISTS rule_targets (
  rule_id INTEGER NOT NULL,
  target_id INTEGER NOT NULL,
  PRIMARY KEY(rule_id, target_id),
  FOREIGN KEY(rule_id) REFERENCES rules(id),
  FOREIGN KEY(target_id) REFERENCES targets(id)
);
CREATE TABLE IF NOT EXISTS delivery_attempts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  delivery_id INTEGER NOT NULL,
  status TEXT NOT NULL,
  request_body TEXT NOT NULL DEFAULT '',
  response_status INTEGER NOT NULL DEFAULT 0,
  response_body TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  finished_at TEXT NOT NULL,
  FOREIGN KEY(delivery_id) REFERENCES deliveries(id)
);
CREATE TABLE IF NOT EXISTS change_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  entity_type TEXT NOT NULL,
  entity_id INTEGER NOT NULL,
  action TEXT NOT NULL,
  before TEXT NOT NULL DEFAULT '',
  after TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_deliveries_status ON deliveries(status, next_retry_at);
CREATE INDEX IF NOT EXISTS idx_deliveries_event ON deliveries(event_id);
CREATE INDEX IF NOT EXISTS idx_change_logs_entity ON change_logs(entity_type, entity_id);
`
	if _, err := d.conn.Exec(schema); err != nil {
		return err
	}
	if _, err := d.conn.Exec("INSERT OR IGNORE INTO schema_version(version) VALUES (1)"); err != nil {
		return err
	}
	return nil
}

// tx 辅助函数：在事务中执行 fn。
func tx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	t, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(t); err != nil {
		t.Rollback()
		return err
	}
	return t.Commit()
}
