package store

import (
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

func scanUser(r *sql.Row) (*User, error) {
	u := &User{}
	var lastLogin sql.NullTime
	if err := r.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt, &lastLogin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if lastLogin.Valid {
		u.LastLogin = &lastLogin.Time
	}
	return u, nil
}

func (db *DB) CreateUser(u *User) error {
	res, err := db.sql.Exec(
		`INSERT INTO users(username, password_hash, role, status, created_at) VALUES(?,?,?,?,?)`,
		u.Username, u.PasswordHash, u.Role, u.Status, u.CreatedAt,
	)
	if err != nil {
		return err
	}
	u.ID, err = res.LastInsertId()
	return err
}

func (db *DB) GetUserByID(id int64) (*User, error) {
	return scanUser(db.sql.QueryRow(
		`SELECT id, username, password_hash, role, status, created_at, last_login FROM users WHERE id=?`, id))
}

func (db *DB) GetUserByName(name string) (*User, error) {
	return scanUser(db.sql.QueryRow(
		`SELECT id, username, password_hash, role, status, created_at, last_login FROM users WHERE username=?`, name))
}

func (db *DB) CountUsers() (int64, error) {
	var n int64
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (db *DB) ListUsers() ([]*User, error) {
	rows, err := db.sql.Query(
		`SELECT id, username, password_hash, role, status, created_at, last_login FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u := &User{}
		var lastLogin sql.NullTime
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt, &lastLogin); err != nil {
			return nil, err
		}
		if lastLogin.Valid {
			u.LastLogin = &lastLogin.Time
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (db *DB) UpdateUserRole(id int64, role string) error {
	_, err := db.sql.Exec(`UPDATE users SET role=? WHERE id=?`, role, id)
	return err
}

func (db *DB) UpdateUserStatus(id int64, status string) error {
	_, err := db.sql.Exec(`UPDATE users SET status=? WHERE id=?`, status, id)
	return err
}

func (db *DB) DeleteUser(id int64) error {
	res, err := db.sql.Exec(`DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = db.sql.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
	return err
}

func (db *DB) UpdateUserLastLogin(id int64) {
	_, _ = db.sql.Exec(`UPDATE users SET last_login=? WHERE id=?`, time.Now(), id)
}

func (db *DB) UpdateUserPassword(id int64, hash string) error {
	res, err := db.sql.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- 会话 ----

func (db *DB) CreateSession(s *Session) error {
	_, err := db.sql.Exec(`INSERT INTO sessions(token, user_id, expires_at) VALUES(?,?,?)`,
		s.Token, s.UserID, s.ExpiresAt)
	return err
}

func (db *DB) GetSession(token string) (*Session, error) {
	s := &Session{}
	err := db.sql.QueryRow(`SELECT token, user_id, expires_at FROM sessions WHERE token=?`, token).
		Scan(&s.Token, &s.UserID, &s.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

func (db *DB) DeleteSession(token string) error {
	_, err := db.sql.Exec(`DELETE FROM sessions WHERE token=?`, token)
	return err
}

func (db *DB) DeleteExpiredSessions() {
	_, _ = db.sql.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now())
}

func (db *DB) DeleteUserSessions(userID int64) error {
	_, err := db.sql.Exec(`DELETE FROM sessions WHERE user_id=?`, userID)
	return err
}

// LoginLimit 是持久化的登录失败计数。把它放在数据库而不是进程内 map，
// 可以让重启/更新后仍然保留锁定状态，也避免多实例时每个实例各自放行爆破。
type LoginLimit struct {
	Failures    int
	LockedUntil *time.Time
	LastFailed  *time.Time
}

func (db *DB) GetLoginLimit(key string) (*LoginLimit, error) {
	limit := &LoginLimit{}
	var lockedUntil, lastFailed sql.NullTime
	err := db.sql.QueryRow(
		`SELECT failures, locked_until, last_failed_at FROM login_limits WHERE key=?`, key,
	).Scan(&limit.Failures, &lockedUntil, &lastFailed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lockedUntil.Valid {
		limit.LockedUntil = &lockedUntil.Time
	}
	if lastFailed.Valid {
		limit.LastFailed = &lastFailed.Time
	}
	return limit, nil
}

// RegisterLoginFailure 原子地记录一次失败，并返回当前是否已进入锁定。
func (db *DB) RegisterLoginFailure(key string, maxFails int, lockFor, window time.Duration) (bool, error) {
	if maxFails < 1 {
		maxFails = 1
	}
	now := time.Now()
	tx, err := db.sql.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var failures int
	var lockedUntil, lastFailed sql.NullTime
	err = tx.QueryRow(
		`SELECT failures, locked_until, last_failed_at FROM login_limits WHERE key=?`, key,
	).Scan(&failures, &lockedUntil, &lastFailed)
	if errors.Is(err, sql.ErrNoRows) {
		failures = 0
	} else if err != nil {
		return false, err
	}
	if lockedUntil.Valid && lockedUntil.Time.After(now) {
		return true, nil
	}
	// 锁定期结束或观察窗口已过，从零开始计算，避免旧失败次数
	// 在很久之后突然触发新的锁定。
	if (lockedUntil.Valid && !lockedUntil.Time.After(now)) ||
		(lastFailed.Valid && window > 0 && now.Sub(lastFailed.Time) > window) {
		failures = 0
	}
	failures++
	var newLocked any
	locked := failures >= maxFails
	if locked {
		newLocked = now.Add(lockFor)
	}
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.Exec(
			`INSERT INTO login_limits(key, failures, locked_until, last_failed_at) VALUES(?,?,?,?)`,
			key, failures, newLocked, now)
	} else {
		_, err = tx.Exec(
			`UPDATE login_limits SET failures=?, locked_until=?, last_failed_at=? WHERE key=?`,
			failures, newLocked, now, key)
	}
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return locked, nil
}

func (db *DB) ClearLoginLimit(key string) error {
	_, err := db.sql.Exec(`DELETE FROM login_limits WHERE key=?`, key)
	return err
}

func (db *DB) DeleteExpiredLoginLimits() {
	_, _ = db.sql.Exec(
		`DELETE FROM login_limits WHERE (locked_until IS NULL OR locked_until < ?) AND (last_failed_at IS NULL OR last_failed_at < ?)`,
		time.Now(), time.Now().Add(-24*time.Hour))
}
