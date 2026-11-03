package config

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	TaskHistoryRetention   = 30 * 24 * time.Hour
	TaskHistoryLimit       = 1000
	MetricHistoryRetention = 7 * 24 * time.Hour
	metricBucketMillis     = int64(30_000)
)

var ErrHistoryStoreUnavailable = errors.New("sqlite history store is not initialized")

// Queue transitions only rewrite their legacy compatibility tables. In
// particular they must not replace unrelated instance or image metadata.
func saveTaskQueueCompatibility(tasks []SavedTask) error {
	dbMu.Lock()
	defer dbMu.Unlock()
	if AppConfig == nil {
		return ErrHistoryStoreUnavailable
	}
	AppConfig.Tasks = tasks
	if db == nil {
		return ErrHistoryStoreUnavailable
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"task_extra_ports", "task_nat_port_mappings", "tasks"} {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return err
		}
	}
	if err := saveTasksDB(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// History tables are deliberately independent of SaveConfig's replace-all tables.
func ensureHistorySchema() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS history_state (key TEXT PRIMARY KEY, value INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS task_history (id TEXT PRIMARY KEY, sequence INTEGER NOT NULL, updated_at INTEGER NOT NULL, finished_at INTEGER NOT NULL DEFAULT 0, data TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS task_history_finished ON task_history(finished_at, sequence)`,
		`CREATE TABLE IF NOT EXISTS metric_history (kind TEXT NOT NULL, series TEXT NOT NULL, sampled_at INTEGER NOT NULL, data TEXT NOT NULL, PRIMARY KEY(kind, series, sampled_at))`,
		`CREATE INDEX IF NOT EXISTS metric_history_time ON metric_history(sampled_at)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("initialize history schema: %w", err)
		}
	}
	return nil
}

type TaskHistoryRecord struct {
	ID         string
	Sequence   int
	FinishedAt int64
	Data       json.RawMessage
}

// ReserveTaskID never lowers its high-water mark, including after history purge.
func ReserveTaskID(minimum int) (int, error) {
	dbMu.Lock()
	defer dbMu.Unlock()
	if db == nil {
		return 0, ErrHistoryStoreUnavailable
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var next int
	if err := tx.QueryRow(`SELECT value FROM history_state WHERE key = 'next_task_id'`).Scan(&next); err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if next < minimum {
		next = minimum
	}
	if _, err := tx.Exec(`INSERT INTO history_state(key,value) VALUES ('next_task_id',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, next+1); err != nil {
		return 0, err
	}
	return next, tx.Commit()
}

func SaveTaskHistory(records []TaskHistoryRecord, nextID int) error {
	dbMu.Lock()
	defer dbMu.Unlock()
	if db == nil {
		return ErrHistoryStoreUnavailable
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, record := range records {
		if !json.Valid(record.Data) {
			return fmt.Errorf("invalid task history JSON for %s", record.ID)
		}
		if _, err := tx.Exec(`INSERT INTO task_history(id,sequence,updated_at,finished_at,data) VALUES (?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET sequence=excluded.sequence,updated_at=excluded.updated_at,finished_at=excluded.finished_at,data=excluded.data`, record.ID, record.Sequence, time.Now().UnixMilli(), record.FinishedAt, string(record.Data)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO history_state(key,value) VALUES ('next_task_id',?) ON CONFLICT(key) DO UPDATE SET value=MAX(value,excluded.value)`, nextID); err != nil {
		return err
	}
	if err := purgeTaskHistory(tx, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func purgeTaskHistory(tx *sql.Tx, now time.Time) error {
	if _, err := tx.Exec(`DELETE FROM task_history WHERE finished_at > 0 AND finished_at < ?`, now.Add(-TaskHistoryRetention).UnixMilli()); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM task_history WHERE finished_at > 0 AND id NOT IN (SELECT id FROM task_history WHERE finished_at > 0 ORDER BY sequence DESC LIMIT ?)`, TaskHistoryLimit)
	return err
}

func LoadTaskHistory() ([]TaskHistoryRecord, int, error) {
	dbMu.Lock()
	defer dbMu.Unlock()
	if db == nil {
		return nil, 0, ErrHistoryStoreUnavailable
	}
	rows, err := db.Query(`SELECT id,sequence,finished_at,data FROM task_history ORDER BY sequence`)
	if err != nil {
		return nil, 0, err
	}
	var records []TaskHistoryRecord
	for rows.Next() {
		var record TaskHistoryRecord
		var data string
		if err := rows.Scan(&record.ID, &record.Sequence, &record.FinishedAt, &data); err != nil {
			rows.Close()
			return nil, 0, err
		}
		record.Data = json.RawMessage(data)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()
	var next int
	if err := db.QueryRow(`SELECT value FROM history_state WHERE key = 'next_task_id'`).Scan(&next); err != nil && err != sql.ErrNoRows {
		return nil, 0, err
	}
	return records, next, nil
}

func DeleteTaskHistory(id string) error {
	dbMu.Lock()
	defer dbMu.Unlock()
	if db == nil {
		return ErrHistoryStoreUnavailable
	}
	_, err := db.Exec(`DELETE FROM task_history WHERE id = ?`, id)
	return err
}

type MetricHistoryRecord struct {
	Series string
	Data   json.RawMessage
}

// A 30s bucket bounds each series to approximately 20,161 records per week,
// including repeated service starts and duplicate samples within one interval.
func SaveMetricHistory(kind, series string, sampledAt int64, data json.RawMessage) error {
	if kind != "host" && kind != "container" || series == "" || !json.Valid(data) {
		return fmt.Errorf("invalid metric history record")
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	if db == nil {
		return ErrHistoryStoreUnavailable
	}
	bucket := sampledAt / metricBucketMillis * metricBucketMillis
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO metric_history(kind,series,sampled_at,data) VALUES (?,?,?,?) ON CONFLICT(kind,series,sampled_at) DO UPDATE SET data=excluded.data`, kind, series, bucket, string(data)); err != nil {
		return err
	}
	// The indexed time delete is small: expired rows are removed on each sample.
	if _, err := tx.Exec(`DELETE FROM metric_history WHERE sampled_at < ?`, time.Now().Add(-MetricHistoryRetention).UnixMilli()-metricBucketMillis); err != nil {
		return err
	}
	return tx.Commit()
}

func LoadMetricHistory(kind, series string) ([]MetricHistoryRecord, error) {
	dbMu.Lock()
	defer dbMu.Unlock()
	if db == nil {
		return nil, ErrHistoryStoreUnavailable
	}
	query := `SELECT series,data FROM metric_history WHERE kind=? AND sampled_at>=?`
	args := []interface{}{kind, time.Now().Add(-MetricHistoryRetention).UnixMilli() - metricBucketMillis}
	if series != "" {
		query += ` AND series=?`
		args = append(args, series)
	}
	rows, err := db.Query(query+` ORDER BY sampled_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []MetricHistoryRecord
	for rows.Next() {
		var record MetricHistoryRecord
		var data string
		if err := rows.Scan(&record.Series, &data); err != nil {
			return nil, err
		}
		record.Data = json.RawMessage(data)
		records = append(records, record)
	}
	return records, rows.Err()
}
