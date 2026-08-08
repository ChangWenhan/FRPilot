package store

import "time"

type AuditLog struct {
	ID       int64     `json:"id"`
	TS       time.Time `json:"ts"`
	UserID   int64     `json:"userId"`
	Username string    `json:"username"`
	Action   string    `json:"action"`
	Target   string    `json:"target"`
	Detail   string    `json:"detail"`
}

// Log 写入一条审计记录（登录/注册/设置修改/清理/体检等全部操作留痕）。
func (db *DB) Log(userID int64, username, action, target, detail string) error {
	_, err := db.sql.Exec(
		`INSERT INTO audit_logs(ts, user_id, username, action, target, detail) VALUES(?,?,?,?,?,?)`,
		time.Now(), userID, username, action, target, detail)
	return err
}

func (db *DB) ListAudit(limit int) ([]*AuditLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.sql.Query(
		`SELECT id, ts, user_id, username, action, target, detail FROM audit_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AuditLog
	for rows.Next() {
		a := &AuditLog{}
		if err := rows.Scan(&a.ID, &a.TS, &a.UserID, &a.Username, &a.Action, &a.Target, &a.Detail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
