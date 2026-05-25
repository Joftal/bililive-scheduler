package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/kira1928/bililive-scheduler/internal/model"
)

var ErrNotFound = errors.New("task not found")

type Store struct {
	db *sql.DB
	// TaskMu serializes all task state transitions (read-modify-write cycles)
	// between the cron engine and API handlers to prevent lost updates.
	TaskMu sync.Mutex
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

const taskColumns = `id, name, room_id, room_url, enabled, state,
                     next_fire_at, current_live_start, monitor_until, last_error, retry_count, max_retries,
                     created_at, updated_at, schedules, current_schedule_idx, next_fire_schedule_idx`

func (s *Store) Create(t *model.ScheduleTask) error {
	schedulesJSON, err := json.Marshal(t.Schedules)
	if err != nil {
		return fmt.Errorf("marshal schedules: %w", err)
	}
	result, err := s.db.Exec(
		`INSERT INTO schedule_tasks (name, room_id, room_url, enabled, state,
		  next_fire_at, next_fire_schedule_idx,
		  max_retries, created_at, updated_at, schedules, current_schedule_idx)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.RoomID, t.RoomURL,
		boolToInt(t.Enabled), string(t.State),
		t.NextFireAt, t.NextFireScheduleIdx,
		t.MaxRetries, t.CreatedAt, t.UpdatedAt,
		string(schedulesJSON), t.CurrentScheduleIdx,
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	t.ID = id
	return nil
}

func (s *Store) Get(id int64) (*model.ScheduleTask, error) {
	row := s.db.QueryRow(
		`SELECT `+taskColumns+` FROM schedule_tasks WHERE id = ?`, id,
	)
	t, err := scanTaskFrom(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

func (s *Store) List(state string, enabled *bool) ([]*model.ScheduleTask, error) {
	query := `SELECT ` + taskColumns + ` FROM schedule_tasks WHERE 1=1`
	args := []any{}

	if state != "" {
		query += " AND state = ?"
		args = append(args, state)
	}
	if enabled != nil {
		query += " AND enabled = ?"
		args = append(args, boolToInt(*enabled))
	}

	query += " ORDER BY id ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*model.ScheduleTask
	for rows.Next() {
		t, err := scanTaskFrom(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) Update(t *model.ScheduleTask) error {
	t.UpdatedAt = time.Now()
	schedulesJSON, err := json.Marshal(t.Schedules)
	if err != nil {
		return fmt.Errorf("marshal schedules: %w", err)
	}
	_, err = s.db.Exec(
		`UPDATE schedule_tasks SET
			name = ?, room_id = ?, room_url = ?,
			enabled = ?, state = ?, next_fire_at = ?, current_live_start = ?, monitor_until = ?,
			last_error = ?, retry_count = ?, max_retries = ?, updated_at = ?,
			schedules = ?, current_schedule_idx = ?, next_fire_schedule_idx = ?
		 WHERE id = ?`,
		t.Name, t.RoomID, t.RoomURL,
		boolToInt(t.Enabled), string(t.State), t.NextFireAt, t.CurrentLiveStart, t.MonitorUntil,
		t.LastError, t.RetryCount, t.MaxRetries, t.UpdatedAt,
		string(schedulesJSON), t.CurrentScheduleIdx, t.NextFireScheduleIdx, t.ID,
	)
	return err
}

func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec("DELETE FROM schedule_tasks WHERE id = ?", id)
	return err
}

func (s *Store) GetDueTasks(now time.Time) ([]*model.ScheduleTask, error) {
	rows, err := s.db.Query(
		`SELECT `+taskColumns+`
		 FROM schedule_tasks
		 WHERE enabled = 1 AND state IN ('pending', 'waiting') AND (next_fire_at IS NULL OR next_fire_at <= ?)
		 ORDER BY next_fire_at ASC`, now,
	)
	if err != nil {
		return nil, fmt.Errorf("query due tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*model.ScheduleTask
	for rows.Next() {
		t, err := scanTaskFrom(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) GetRecordingTasks() ([]*model.ScheduleTask, error) {
	rows, err := s.db.Query(
		`SELECT `+taskColumns+`
		 FROM schedule_tasks WHERE state = 'recording' ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query recording tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*model.ScheduleTask
	for rows.Next() {
		t, err := scanTaskFrom(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) GetReschedulableTasks() ([]*model.ScheduleTask, error) {
	rows, err := s.db.Query(
		`SELECT `+taskColumns+`
		 FROM schedule_tasks
		 WHERE enabled = 1 AND state IN ('completed', 'pending', 'error')
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query reschedulable tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*model.ScheduleTask
	for rows.Next() {
		t, err := scanTaskFrom(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) GetCounts() (total, recording, waiting, errored int, err error) {
	err = s.db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN state = 'recording' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN state IN ('pending', 'waiting') AND enabled = 1 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN state = 'error' THEN 1 ELSE 0 END), 0)
		FROM schedule_tasks
	`).Scan(&total, &recording, &waiting, &errored)
	return
}

func (s *Store) AddExecution(exec *model.TaskExecution) error {
	result, err := s.db.Exec(
		`INSERT INTO task_executions (task_id, start_time, end_time, state, error)
		 VALUES (?, ?, ?, ?, ?)`,
		exec.TaskID, exec.StartTime, exec.EndTime, string(exec.State), exec.Error,
	)
	if err != nil {
		return fmt.Errorf("insert execution: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	exec.ID = id
	return nil
}

func (s *Store) UpdateExecution(exec *model.TaskExecution) error {
	_, err := s.db.Exec(
		`UPDATE task_executions SET end_time = ?, state = ?, error = ? WHERE id = ?`,
		exec.EndTime, string(exec.State), exec.Error, exec.ID,
	)
	return err
}

func (s *Store) GetExecutions(taskID int64, limit int) ([]*model.TaskExecution, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, task_id, start_time, end_time, state, error
		 FROM task_executions WHERE task_id = ? ORDER BY id DESC LIMIT ?`,
		taskID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query executions: %w", err)
	}
	defer rows.Close()

	var execs []*model.TaskExecution
	for rows.Next() {
		e := &model.TaskExecution{}
		var state string
		var errStr sql.NullString
		if err := rows.Scan(&e.ID, &e.TaskID, &e.StartTime, &e.EndTime, &state, &errStr); err != nil {
			return nil, fmt.Errorf("scan execution: %w", err)
		}
		e.State = model.TaskState(state)
		e.Error = errStr.String
		execs = append(execs, e)
	}
	return execs, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTaskFrom(s scanner) (*model.ScheduleTask, error) {
	t := &model.ScheduleTask{}
	var state string
	var enabled int
	var lastError sql.NullString
	var schedulesStr sql.NullString
	if err := s.Scan(
		&t.ID, &t.Name, &t.RoomID, &t.RoomURL,
		&enabled, &state, &t.NextFireAt, &t.CurrentLiveStart, &t.MonitorUntil,
		&lastError, &t.RetryCount, &t.MaxRetries, &t.CreatedAt, &t.UpdatedAt,
		&schedulesStr, &t.CurrentScheduleIdx, &t.NextFireScheduleIdx,
	); err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	t.Enabled = enabled != 0
	t.State = model.TaskState(state)
	t.LastError = lastError.String
	t.Schedules = parseSchedules(schedulesStr.String)
	return t, nil
}

func parseSchedules(s string) []model.ScheduleEntry {
	if s == "" || s == "null" {
		return []model.ScheduleEntry{}
	}
	var entries []model.ScheduleEntry
	if err := json.Unmarshal([]byte(s), &entries); err != nil {
		log.Printf("[db] parse schedules JSON error: %v (input: %q)", err, s)
		return []model.ScheduleEntry{}
	}
	if entries == nil {
		return []model.ScheduleEntry{}
	}
	return entries
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GetConfig retrieves a config value by key. Returns defaultValue if not found.
func (s *Store) GetConfig(key, defaultValue string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return defaultValue, nil
		}
		return defaultValue, err
	}
	return value, nil
}

// SetConfig upserts a config key-value pair.
func (s *Store) SetConfig(key, value string) error {
	_, err := s.db.Exec(
		"INSERT INTO config (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?",
		key, value, value,
	)
	return err
}
