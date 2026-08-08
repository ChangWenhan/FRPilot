package store

import (
	"database/sql"
	"errors"
	"time"
)

type Snapshot struct {
	MachineID int64     `json:"machineId"`
	TS        time.Time `json:"ts"`
	Data      string    `json:"data"` // 采集结果 JSON（各模块）
}

type MetricPoint struct {
	TS         time.Time `json:"ts"`
	CPU        float64   `json:"cpu"`
	Mem        float64   `json:"mem"`
	Disk       float64   `json:"disk"`
	GPUUtil    float64   `json:"gpuUtil"`
	GPUMem     float64   `json:"gpuMem"`
	GPUTemp    float64   `json:"gpuTemp"`
	NetIn      float64   `json:"netIn"`  // 速率 B/s
	NetOut     float64   `json:"netOut"` // 速率 B/s
	Conns      int       `json:"conns"`
}

// 历史保留天数
const MetricRetentionDays = 30

func (db *DB) SaveSnapshot(machineID int64, data string) error {
	_, err := db.sql.Exec(
		`INSERT INTO machine_snapshots(machine_id, ts, data) VALUES(?,?,?)
		 ON CONFLICT(machine_id) DO UPDATE SET ts=excluded.ts, data=excluded.data`,
		machineID, time.Now(), data)
	return err
}

func (db *DB) GetSnapshot(machineID int64) (*Snapshot, error) {
	s := &Snapshot{}
	err := db.sql.QueryRow(
		`SELECT machine_id, ts, data FROM machine_snapshots WHERE machine_id=?`, machineID).
		Scan(&s.MachineID, &s.TS, &s.Data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

func (db *DB) SaveMetricFor(machineID int64, m *MetricPoint) error {
	_, err := db.sql.Exec(
		`INSERT INTO metrics_history(machine_id, ts, cpu, mem, disk, gpu_util, gpu_mem, gpu_temp, net_in, net_out, conns)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		machineID, m.TS, m.CPU, m.Mem, m.Disk, m.GPUUtil, m.GPUMem, m.GPUTemp, m.NetIn, m.NetOut, m.Conns)
	return err
}

func (db *DB) GetMetrics(machineID int64, hours int) ([]*MetricPoint, error) {
	if hours <= 0 {
		hours = 24
	}
	rows, err := db.sql.Query(
		`SELECT ts, cpu, mem, disk, gpu_util, gpu_mem, gpu_temp, net_in, net_out, conns
		 FROM metrics_history WHERE machine_id=? AND ts >= ?
		 ORDER BY ts`, machineID, time.Now().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*MetricPoint
	for rows.Next() {
		m := &MetricPoint{}
		if err := rows.Scan(&m.TS, &m.CPU, &m.Mem, &m.Disk, &m.GPUUtil, &m.GPUMem, &m.GPUTemp, &m.NetIn, &m.NetOut, &m.Conns); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) CleanupMetrics() error {
	_, err := db.sql.Exec(`DELETE FROM metrics_history WHERE ts < ?`,
		time.Now().Add(-time.Duration(MetricRetentionDays)*24*time.Hour))
	return err
}
