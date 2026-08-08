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
