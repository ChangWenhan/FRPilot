package store

import (
	"database/sql"
	"errors"
	"time"
)

type TrafficPoint struct {
	TS       time.Time `json:"ts"`
	Proxy    string    `json:"proxy"`
	MachineID int64    `json:"machineId"`
	InBytes  int64     `json:"inBytes"`
	OutBytes int64     `json:"outBytes"`
	RateIn   float64   `json:"rateIn"`
	RateOut  float64   `json:"rateOut"`
}

// SaveTraffic 写入一次流量采样（每 proxy 一行）。
func (db *DB) SaveTraffic(pts []*TrafficPoint) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(
		`INSERT INTO traffic_history(ts, proxy, machine_id, in_bytes, out_bytes, rate_in, rate_out)
		 VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range pts {
		if _, err := stmt.Exec(p.TS, p.Proxy, p.MachineID, p.InBytes, p.OutBytes, p.RateIn, p.RateOut); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetTrafficHistory 按时间段查询流量采样（可过滤单 proxy）。
func (db *DB) GetTrafficHistory(proxy string, hours int) ([]*TrafficPoint, error) {
	if hours <= 0 {
		hours = 24
	}
	var rows *sql.Rows
	var err error
	from := time.Now().Add(-time.Duration(hours) * time.Hour)
	if proxy != "" {
		rows, err = db.sql.Query(
			`SELECT ts, proxy, machine_id, in_bytes, out_bytes, rate_in, rate_out
			 FROM traffic_history WHERE proxy=? AND ts>=? ORDER BY ts`, proxy, from)
	} else {
		rows, err = db.sql.Query(
			`SELECT ts, proxy, machine_id, in_bytes, out_bytes, rate_in, rate_out
			 FROM traffic_history WHERE ts>=? ORDER BY ts`, from)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TrafficPoint
	for rows.Next() {
		p := &TrafficPoint{}
		if err := rows.Scan(&p.TS, &p.Proxy, &p.MachineID, &p.InBytes, &p.OutBytes, &p.RateIn, &p.RateOut); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetLatestTraffic 取最近一次采样的全部 proxy 数据（带宽流向分析用）。
func (db *DB) GetLatestTraffic() ([]*TrafficPoint, error) {
	var last time.Time
	err := db.sql.QueryRow(`SELECT MAX(ts) FROM traffic_history`).Scan(&last)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := db.sql.Query(
		`SELECT ts, proxy, machine_id, in_bytes, out_bytes, rate_in, rate_out
		 FROM traffic_history WHERE ts=? ORDER BY (rate_in+rate_out) DESC`, last)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TrafficPoint
	for rows.Next() {
		p := &TrafficPoint{}
		if err := rows.Scan(&p.TS, &p.Proxy, &p.MachineID, &p.InBytes, &p.OutBytes, &p.RateIn, &p.RateOut); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

func (db *DB) CleanupTraffic() error {
	_, err := db.sql.Exec(`DELETE FROM traffic_history WHERE ts < ?`,
		time.Now().Add(-time.Duration(MetricRetentionDays)*24*time.Hour))
	return err
}
