package store

import (
	"database/sql"
	"errors"
	"time"
)

// MachineStatus 状态流转：
//
//	pending   待配置：自动发现的新机器，未填 SSH 凭据（列表可见，不监控）
//	configured 已配置凭据但监控开关未开（保存了凭据，暂不采集）
//	enabled   监控中：凭据就绪 + 开关开启
//	disabled  停用：曾启用后关闭开关（停止采集，历史保留）
const (
	MachinePending   = "pending"
	MachineConfigured = "configured"
	MachineEnabled   = "enabled"
	MachineDisabled  = "disabled"
)

type Machine struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	TunnelPort    int        `json:"tunnelPort"`
	SSHUser       string     `json:"sshUser"`
	SSHPassEnc    string     `json:"-"`
	Status        string     `json:"status"`
	Enabled       bool       `json:"enabled"`
	Notes         string     `json:"notes"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	LastSeenAt    *time.Time `json:"lastSeenAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
}

func scanMachine(r *sql.Row) (*Machine, error) {
	m := &Machine{}
	var enabled int
	var lastSeen, lastSuccess sql.NullTime
	err := r.Scan(&m.ID, &m.Name, &m.TunnelPort, &m.SSHUser, &m.SSHPassEnc, &m.Status,
		&enabled, &m.Notes, &m.CreatedAt, &m.UpdatedAt, &lastSeen, &lastSuccess)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	m.Enabled = enabled != 0
	if lastSeen.Valid {
		m.LastSeenAt = &lastSeen.Time
	}
	if lastSuccess.Valid {
		m.LastSuccessAt = &lastSuccess.Time
	}
	return m, nil
}

func (db *DB) UpsertMachineFromDiscovery(name string, tunnelPort int) (*Machine, bool, error) {
	existing, err := db.GetMachineByName(name)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	now := time.Now()
	m := &Machine{Name: name, TunnelPort: tunnelPort, Status: MachinePending, CreatedAt: now, UpdatedAt: now}
	res, err := db.sql.Exec(
		`INSERT INTO machines(name, tunnel_port, status, enabled, created_at, updated_at) VALUES(?,?,?,0,?,?)`,
		m.Name, m.TunnelPort, m.Status, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return nil, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, false, err
	}
	m.ID = id
	return m, true, nil
}

func (db *DB) GetMachineByID(id int64) (*Machine, error) {
	return scanMachine(db.sql.QueryRow(
		`SELECT id, name, tunnel_port, ssh_user, ssh_pass_enc, status, enabled, notes, created_at, updated_at, last_seen_at, last_success_at
		 FROM machines WHERE id=?`, id))
}

func (db *DB) GetMachineByName(name string) (*Machine, error) {
	return scanMachine(db.sql.QueryRow(
		`SELECT id, name, tunnel_port, ssh_user, ssh_pass_enc, status, enabled, notes, created_at, updated_at, last_seen_at, last_success_at
		 FROM machines WHERE name=?`, name))
}

func (db *DB) ListMachines() ([]*Machine, error) {
	rows, err := db.sql.Query(
		`SELECT id, name, tunnel_port, ssh_user, ssh_pass_enc, status, enabled, notes, created_at, updated_at, last_seen_at, last_success_at
		 FROM machines ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Machine
	for rows.Next() {
		m := &Machine{}
		var enabled int
		var lastSeen, lastSuccess sql.NullTime
		if err := rows.Scan(&m.ID, &m.Name, &m.TunnelPort, &m.SSHUser, &m.SSHPassEnc, &m.Status,
			&enabled, &m.Notes, &m.CreatedAt, &m.UpdatedAt, &lastSeen, &lastSuccess); err != nil {
			return nil, err
		}
		m.Enabled = enabled != 0
		if lastSeen.Valid {
			m.LastSeenAt = &lastSeen.Time
		}
		if lastSuccess.Valid {
			m.LastSuccessAt = &lastSuccess.Time
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMachineCredentials 填写 SSH 凭据；status 从 pending → configured。
// 凭据由调用方先加密为 SSHPassEnc。
func (db *DB) SetMachineCredentials(id int64, sshUser, sshPassEnc string) error {
	res, err := db.sql.Exec(
		`UPDATE machines SET ssh_user=?, ssh_pass_enc=?, status=?, updated_at=? WHERE id=?`,
		sshUser, sshPassEnc, MachineConfigured, time.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetMachineEnabled 开启/关闭监控开关。
// 开启要求凭据已配置；关闭后 status 置 disabled（保留数据），再开则回到 enabled。
func (db *DB) SetMachineEnabled(id int64, enabled bool) (*Machine, error) {
	m, err := db.GetMachineByID(id)
	if err != nil {
		return nil, err
	}
	if enabled && m.SSHUser == "" {
		return nil, errors.New("未配置 SSH 凭据，不能启用监控")
	}
	status := MachineDisabled
	if enabled {
		status = MachineEnabled
	}
	e := 0
	if enabled {
		e = 1
	}
	if _, err := db.sql.Exec(
		`UPDATE machines SET enabled=?, status=?, updated_at=? WHERE id=?`, e, status, time.Now(), id); err != nil {
		return nil, err
	}
	return db.GetMachineByID(id)
}

func (db *DB) TouchMachine(id int64, seen, success bool) {
	if seen {
		_, _ = db.sql.Exec(`UPDATE machines SET last_seen_at=? WHERE id=?`, time.Now(), id)
	}
	if success {
		_, _ = db.sql.Exec(`UPDATE machines SET last_success_at=? WHERE id=?`, time.Now(), id)
	}
}

// ---- 设置（键值） ----

func (db *DB) GetSetting(key string) (string, error) {
	var v string
	err := db.sql.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

func (db *DB) SetSetting(key, value string) error {
	_, err := db.sql.Exec(
		`INSERT INTO settings(key, value) VALUES(?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
