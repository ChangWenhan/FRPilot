package store

import "time"

// RetentionDays 各类历史数据保留天数（统一 30 天，防止无界增长）。
const RetentionDays = 30

// CleanupRetention 清理所有历史表超期数据（指标/流量/审计/体检/诊断/采集日志）。
// 快照表每机器仅一行，无需清理。
func (db *DB) CleanupRetention() error {
	cutoff := time.Now().Add(-time.Duration(RetentionDays) * 24 * time.Hour)
	stmts := []string{
		`DELETE FROM metrics_history WHERE ts < ?`,
		`DELETE FROM traffic_history WHERE ts < ?`,
		`DELETE FROM audit_logs WHERE ts < ?`,
		`DELETE FROM health_reports WHERE ts < ?`,
		`DELETE FROM ai_diagnostics WHERE ts < ?`,
		`DELETE FROM collect_logs WHERE ts < ?`,
	}
	for _, s := range stmts {
		if _, err := db.sql.Exec(s, cutoff); err != nil {
			return err
		}
	}
	// 可选：回收 WAL 文件膨胀（SQLite 自动 checkpoint，此处显式触发）
	_, _ = db.sql.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return nil
}
