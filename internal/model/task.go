package model

import (
	"fmt"
	"time"

	cronlib "github.com/robfig/cron/v3"
)

var cronParser = cronlib.NewParser(cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow)

type TaskState string

const (
	StatePending   TaskState = "pending"
	StateWaiting   TaskState = "waiting"
	StateRecording TaskState = "recording"
	StateCompleted TaskState = "completed"
	StateError     TaskState = "error"
)

type ScheduleTask struct {
	ID               int64      `json:"id"`
	Name             string     `json:"name"`
	RoomID           string     `json:"room_id"`
	RoomURL          string     `json:"room_url"`
	CronExpr         string     `json:"cron_expr"`
	DurationMinutes  int        `json:"duration_min"`
	Enabled          bool       `json:"enabled"`
	State            TaskState  `json:"state"`
	NextFireAt       *time.Time `json:"next_fire_at"`
	CurrentLiveStart *time.Time `json:"current_live_start"`
	LastError        string     `json:"last_error"`
	RetryCount       int        `json:"retry_count"`
	MaxRetries       int        `json:"max_retries"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type CreateTaskRequest struct {
	Name            string `json:"name"`
	RoomID          string `json:"room_id"`
	RoomURL         string `json:"room_url"`
	CronExpr        string `json:"cron_expr"`
	DurationMinutes int    `json:"duration_min"`
	MaxRetries      int    `json:"max_retries"`
}

type UpdateTaskRequest struct {
	Name            *string `json:"name,omitempty"`
	CronExpr        *string `json:"cron_expr,omitempty"`
	DurationMinutes *int    `json:"duration_min,omitempty"`
	MaxRetries      *int    `json:"max_retries,omitempty"`
}

type TaskExecution struct {
	ID        int64      `json:"id"`
	TaskID    int64      `json:"task_id"`
	StartTime time.Time  `json:"start_time"`
	EndTime   *time.Time `json:"end_time"`
	State     TaskState  `json:"state"`
	Error     string     `json:"error,omitempty"`
}

type SchedulerStatus struct {
	Running          bool   `json:"running"`
	TotalTasks       int    `json:"total_tasks"`
	ActiveRecordings int    `json:"active_recordings"`
	WaitingTasks     int    `json:"waiting_tasks"`
	ErrorTasks       int    `json:"error_tasks"`
	BiliAPIReachable bool   `json:"bili_api_reachable"`
	Uptime           string `json:"uptime"`
}

type RoomInfo struct {
	ID       string `json:"id"`
	HostName string `json:"host_name"`
	RoomName string `json:"room_name"`
	URL      string `json:"url"`
	IsLive   bool   `json:"is_live"`
}

func (t *ScheduleTask) Validate() error {
	if t.RoomID == "" {
		return fmt.Errorf("room_id is required")
	}
	if t.CronExpr == "" {
		return fmt.Errorf("cron_expr is required")
	}
	if _, err := cronParser.Parse(t.CronExpr); err != nil {
		return fmt.Errorf("invalid cron_expr %q: %w", t.CronExpr, err)
	}
	if t.DurationMinutes < 0 {
		return fmt.Errorf("duration_min must be >= 0")
	}
	if t.MaxRetries < 0 {
		return fmt.Errorf("max_retries must be >= 0")
	}
	if len(t.Name) > 256 {
		return fmt.Errorf("name must be <= 256 characters")
	}
	return nil
}

func (r *CreateTaskRequest) ToTask() *ScheduleTask {
	maxRetries := r.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}
	return &ScheduleTask{
		Name:            r.Name,
		RoomID:          r.RoomID,
		RoomURL:         r.RoomURL,
		CronExpr:        r.CronExpr,
		DurationMinutes: r.DurationMinutes,
		Enabled:         true,
		State:           StatePending,
		MaxRetries:      maxRetries,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}
