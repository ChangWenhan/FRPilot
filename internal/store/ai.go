package store

import (
	"database/sql"
	"time"
)

type AIDiagnostic struct {
	ID        int64     `json:"id"`
	TS        time.Time `json:"ts"`
	MachineID int64     `json:"machineId"`
	Machine   string    `json:"machine"`
	Overall   string    `json:"overall"`
	Score     int       `json:"score"`
	Text      string    `json:"text"`
	Flagged   bool      `json:"flagged"`
}

func (db *DB) SaveDiagnostic(d *AIDiagnostic) error {
	res, err := db.sql.Exec(
		`INSERT INTO ai_diagnostics(ts, machine_id, machine, report_overall, report_score, text, flagged)
		 VALUES(?,?,?,?,?,?,?)`,
		d.TS, d.MachineID, d.Machine, d.Overall, d.Score, d.Text, map[bool]int{true: 1, false: 0}[d.Flagged])
	if err != nil {
		return err
	}
	d.ID, err = res.LastInsertId()
	return err
}

func (db *DB) ListDiagnostics(machineID int64, limit int) ([]*AIDiagnostic, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	var rows *sql.Rows
	var err error
	if machineID > 0 {
		rows, err = db.sql.Query(
			`SELECT id, ts, machine_id, machine, report_overall, report_score, text, flagged
			 FROM ai_diagnostics WHERE machine_id=? ORDER BY id DESC LIMIT ?`, machineID, limit)
	} else {
		rows, err = db.sql.Query(
			`SELECT id, ts, machine_id, machine, report_overall, report_score, text, flagged
			 FROM ai_diagnostics ORDER BY id DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AIDiagnostic
	for rows.Next() {
		d := &AIDiagnostic{}
		var flagged int
		if err := rows.Scan(&d.ID, &d.TS, &d.MachineID, &d.Machine, &d.Overall, &d.Score, &d.Text, &flagged); err != nil {
			return nil, err
		}
		d.Flagged = flagged != 0
		out = append(out, d)
	}
	return out, rows.Err()
}
