package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/kira1928/bililive-scheduler/internal/model"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(t *model.ScheduleTask) error {
	result, err := s.db.Exec(
		`INSERT INTO schedule_tasks (name, room_id, room_url, cron_expr, duration_min, enabled, state, max_retries, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.RoomID, t.RoomURL, t.CronExpr, t.DurationMinutes,
		boolToInt(t.Enabled), string(t.State), t.MaxRetries, t.CreatedAt, t.UpdatedAt,
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
		`SELECT id, name, room_id, room_url, cron_expr, duration_min, enabled, state,
		        next_fire_at, current_live_start, last_error, retry_count, max_retries,
		        created_at, updated_at
		 FROM schedule_tasks WHERE id = ?`, id,
	)
	return scanTask(row)
}

func (s *Store) List(state string, enabled *bool) ([]*model.ScheduleTask, error) {
	query := `SELECT id, name, room_id, room_url, cron_expr, duration_min, enabled, state,
	                 next_fire_at, current_live_start, last_error, retry_count, max_retries,
	                 created_at, updated_at
	          FROM schedule_tasks WHERE 1=1`
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
		t, err := scanTaskRows(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) Update(t *model.ScheduleTask) error {
	t.UpdatedAt = time.Now()
	_, err := s.db.Exec(
		`UPDATE schedule_tasks SET
			name = ?, room_id = ?, room_url = ?, cron_expr = ?, duration_min = ?,
			enabled = ?, state = ?, next_fire_at = ?, current_live_start = ?,
			last_error = ?, retry_count = ?, max_retries = ?, updated_at = ?
		 WHERE id = ?`,
		t.Name, t.RoomID, t.RoomURL, t.CronExpr, t.DurationMinutes,
		boolToInt(t.Enabled), string(t.State), t.NextFireAt, t.CurrentLiveStart,
		t.LastError, t.RetryCount, t.MaxRetries, t.UpdatedAt, t.ID,
	)
	return err
}

func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec("DELETE FROM schedule_tasks WHERE id = ?", id)
	return err
}

func (s *Store) GetDueTasks(now time.Time) ([]*model.ScheduleTask, error) {
	rows, err := s.db.Query(
		`SELECT id, name, room_id, room_url, cron_expr, duration_min, enabled, state,
		        next_fire_at, current_live_start, last_error, retry_count, max_retries,
		        created_at, updated_at
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
		t, err := scanTaskRows(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) GetRecordingTasks() ([]*model.ScheduleTask, error) {
	trueVal := true
	return s.List("recording", &trueVal)
}

func (s *Store) GetCounts() (total, recording, waiting, errored int, err error) {
	err = s.db.QueryRow("SELECT COUNT(*) FROM schedule_tasks").Scan(&total)
	if err != nil {
		return
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM schedule_tasks WHERE state = 'recording'").Scan(&recording)
	if err != nil {
		return
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM schedule_tasks WHERE state IN ('pending', 'waiting') AND enabled = 1").Scan(&waiting)
	if err != nil {
		return
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM schedule_tasks WHERE state = 'error'").Scan(&errored)
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

func scanTask(row *sql.Row) (*model.ScheduleTask, error) {
	t := &model.ScheduleTask{}
	var state string
	var enabled int
	var lastError sql.NullString
	if err := row.Scan(
		&t.ID, &t.Name, &t.RoomID, &t.RoomURL, &t.CronExpr, &t.DurationMinutes,
		&enabled, &state, &t.NextFireAt, &t.CurrentLiveStart,
		&lastError, &t.RetryCount, &t.MaxRetries, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	t.Enabled = enabled != 0
	t.State = model.TaskState(state)
	t.LastError = lastError.String
	return t, nil
}

func scanTaskRows(rows *sql.Rows) (*model.ScheduleTask, error) {
	t := &model.ScheduleTask{}
	var state string
	var enabled int
	var lastError sql.NullString
	if err := rows.Scan(
		&t.ID, &t.Name, &t.RoomID, &t.RoomURL, &t.CronExpr, &t.DurationMinutes,
		&enabled, &state, &t.NextFireAt, &t.CurrentLiveStart,
		&lastError, &t.RetryCount, &t.MaxRetries, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	t.Enabled = enabled != 0
	t.State = model.TaskState(state)
	t.LastError = lastError.String
	return t, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
