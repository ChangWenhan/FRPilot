package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	sql *sql.DB
	key []byte
}

type User struct {
	ID           int64      `json:"id"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"`
	Role         string     `json:"role"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastLogin    *time.Time `json:"lastLogin"`
}

type Session struct {
	Token     string    `json:"token"`
	UserID    int64     `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func Open(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	sqlDB, err := sql.Open("sqlite", filepath.Join(dataDir, "monitor.db"))
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	// SQLite 连接级优化：WAL 提升并发读写、busy_timeout 避免写锁冲突、NORMAL 平衡持久性与性能
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-8000", // 8MB 页缓存
	} {
		if _, err := sqlDB.Exec(pragma); err != nil {
			return nil, err
		}
	}
	key, err := loadOrCreateKey(filepath.Join(dataDir, "secret.key"))
	if err != nil {
		return nil, err
	}
	db := &DB{sql: sqlDB, key: key}
	if err := db.migrate(); err != nil {
		return nil, err
	}
	return db, nil
}

func loadOrCreateKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		return b, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func (db *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'user',
			status TEXT NOT NULL DEFAULT 'approved',
			created_at DATETIME NOT NULL,
			last_login DATETIME)`,
		`CREATE TABLE IF NOT EXISTS sessions(
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at DATETIME NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS login_limits(
			key TEXT PRIMARY KEY,
			failures INTEGER NOT NULL DEFAULT 0,
			locked_until DATETIME,
			last_failed_at DATETIME)`,
		`CREATE TABLE IF NOT EXISTS machines(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			tunnel_port INTEGER NOT NULL DEFAULT 0,
			ssh_user TEXT NOT NULL DEFAULT '',
			ssh_pass_enc TEXT NOT NULL DEFAULT '',
			sudo_pass_enc TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			enabled INTEGER NOT NULL DEFAULT 0,
			notes TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			last_seen_at DATETIME,
			last_success_at DATETIME)`,
		`CREATE TABLE IF NOT EXISTS audit_logs(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts DATETIME NOT NULL,
			user_id INTEGER NOT NULL DEFAULT 0,
			username TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			target TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS settings(
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS machine_snapshots(
			machine_id INTEGER PRIMARY KEY,
			ts DATETIME NOT NULL,
			data TEXT NOT NULL DEFAULT '{}')`,
		`CREATE TABLE IF NOT EXISTS metrics_history(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			machine_id INTEGER NOT NULL,
			ts DATETIME NOT NULL,
			cpu REAL NOT NULL DEFAULT 0,
			mem REAL NOT NULL DEFAULT 0,
			disk REAL NOT NULL DEFAULT 0,
			gpu_util REAL NOT NULL DEFAULT -1,
			gpu_mem REAL NOT NULL DEFAULT -1,
			gpu_temp REAL NOT NULL DEFAULT -1,
			net_in REAL NOT NULL DEFAULT 0,
			net_out REAL NOT NULL DEFAULT 0,
			conns INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_machine_ts ON metrics_history(machine_id, ts)`,
		`CREATE TABLE IF NOT EXISTS traffic_history(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts DATETIME NOT NULL,
			proxy TEXT NOT NULL DEFAULT '',
			machine_id INTEGER NOT NULL DEFAULT 0,
			in_bytes INTEGER NOT NULL DEFAULT 0,
			out_bytes INTEGER NOT NULL DEFAULT 0,
			rate_in REAL NOT NULL DEFAULT 0,
			rate_out REAL NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS idx_traffic_ts ON traffic_history(ts)`,
		`CREATE INDEX IF NOT EXISTS idx_traffic_proxy_ts ON traffic_history(proxy, ts)`,
		`CREATE TABLE IF NOT EXISTS collect_logs(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts DATETIME NOT NULL,
			machine_id INTEGER NOT NULL,
			level TEXT NOT NULL DEFAULT 'info',
			module TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS health_reports(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts DATETIME NOT NULL,
			machine_id INTEGER NOT NULL,
			machine TEXT NOT NULL DEFAULT '',
			score INTEGER NOT NULL DEFAULT 0,
			overall TEXT NOT NULL DEFAULT 'pass',
			items_json TEXT NOT NULL DEFAULT '[]')`,
		`CREATE INDEX IF NOT EXISTS idx_health_machine ON health_reports(machine_id, id)`,
		`CREATE TABLE IF NOT EXISTS ai_diagnostics(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts DATETIME NOT NULL,
			machine_id INTEGER NOT NULL,
			machine TEXT NOT NULL DEFAULT '',
			report_overall TEXT NOT NULL DEFAULT '',
			report_score INTEGER NOT NULL DEFAULT 0,
			text TEXT NOT NULL DEFAULT '',
			flagged INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_machine ON ai_diagnostics(machine_id, id)`,
	}
	for _, s := range stmts {
		if _, err := db.sql.Exec(s); err != nil {
			return err
		}
	}
	// 增量迁移：老库的 machines 表补 sudo_pass_enc 列。
	if err := db.ensureColumn("machines", "sudo_pass_enc", `ALTER TABLE machines ADD COLUMN sudo_pass_enc TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	return nil
}

func (db *DB) ensureColumn(table, column, alterSQL string) error {
	rows, err := db.sql.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.sql.Exec(alterSQL)
	return err
}

func (db *DB) Close() error { return db.sql.Close() }

func (db *DB) Now() time.Time { return time.Now() }

// ---- 密码加密（SSH 凭据落盘用 AES-GCM，密钥本机隔离） ----

func (db *DB) EncryptSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(db.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (db *DB) DecryptSecret(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(db.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("密文太短")
	}
	out, err := gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
