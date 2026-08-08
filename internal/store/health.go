package store

import (
	"database/sql"
	"errors"
	"time"
)

type HealthReport struct {
	ID         int64     `json:"id"`
	TS         time.Time `json:"ts"`
	MachineID  int64     `json:"machineId"`
	Machine    string    `json:"machine"`
	Score      int       `json:"score"`
	Overall    string    `json:"overall"` // pass | warn | fail
	ItemsJSON  string    `json:"itemsJson"`
}

func (db *DB) SaveHealthReport(r *HealthReport) error {
	res, err := db.sql.Exec(
		`INSERT INTO health_reports(ts, machine_id, machine, score, overall, items_json)
		 VALUES(?,?,?,?,?,?)`,
		r.TS, r.MachineID, r.Machine, r.Score, r.Overall, r.ItemsJSON)
	if err != nil {
		return err
	}
	r.ID, err = res.LastInsertId()
	return err
}

func (db *DB) ListHealthReports(machineID int64, limit int) ([]*HealthReport, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rows *sql.Rows
	var err error
	if machineID > 0 {
		rows, err = db.sql.Query(
			`SELECT id, ts, machine_id, machine, score, overall, items_json
			 FROM health_reports WHERE machine_id=? ORDER BY id DESC LIMIT ?`, machineID, limit)
	} else {
		rows, err = db.sql.Query(
			`SELECT id, ts, machine_id, machine, score, overall, items_json
			 FROM health_reports ORDER BY id DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*HealthReport
	for rows.Next() {
		r := &HealthReport{}
		if err := rows.Scan(&r.ID, &r.TS, &r.MachineID, &r.Machine, &r.Score, &r.Overall, &r.ItemsJSON); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) GetHealthReport(id int64) (*HealthReport, error) {
	r := &HealthReport{}
	err := db.sql.QueryRow(
		`SELECT id, ts, machine_id, machine, score, overall, items_json
		 FROM health_reports WHERE id=?`, id).
		Scan(&r.ID, &r.TS, &r.MachineID, &r.Machine, &r.Score, &r.Overall, &r.ItemsJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r, nil
}
