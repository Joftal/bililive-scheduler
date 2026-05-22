package model

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	cronlib "github.com/robfig/cron/v3"
)

var cronParser = cronlib.NewParser(cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow)

var timeRegex = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

type TaskState string

const (
	StatePending   TaskState = "pending"
	StateWaiting   TaskState = "waiting"
	StateRecording TaskState = "recording"
	StateCompleted TaskState = "completed"
	StateError     TaskState = "error"
)

// ScheduleEntry represents a single time slot in a multi-schedule task.
type ScheduleEntry struct {
	Days        []int  `json:"days"`         // 0=Sun, 1=Mon, ..., 6=Sat
	StartTime   string `json:"start_time"`   // "HH:MM" format, 24-hour
	DurationMin int    `json:"duration_min"` // 0 = until stream ends
	CronExpr    string `json:"cron_expr"`    // auto-generated, read-only for clients
}

type ScheduleTask struct {
	ID                  int64           `json:"id"`
	Name                string          `json:"name"`
	RoomID              string          `json:"room_id"`
	RoomURL             string          `json:"room_url"`
	CronExpr            string          `json:"cron_expr"`
	DurationMinutes     int             `json:"duration_min"`
	Enabled             bool            `json:"enabled"`
	State               TaskState       `json:"state"`
	NextFireAt          *time.Time      `json:"next_fire_at"`
	CurrentLiveStart    *time.Time      `json:"current_live_start"`
	LastError           string          `json:"last_error"`
	RetryCount          int             `json:"retry_count"`
	MaxRetries          int             `json:"max_retries"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	Schedules           []ScheduleEntry `json:"schedules"`
	CurrentScheduleIdx  int             `json:"current_schedule_idx"`
	NextFireScheduleIdx int             `json:"next_fire_schedule_idx"`
}

type CreateTaskRequest struct {
	Name            string          `json:"name"`
	RoomID          string          `json:"room_id"`
	RoomURL         string          `json:"room_url"`
	CronExpr        string          `json:"cron_expr"`
	DurationMinutes int             `json:"duration_min"`
	MaxRetries      int             `json:"max_retries"`
	Schedules       []ScheduleEntry `json:"schedules"`
}

type UpdateTaskRequest struct {
	Name            *string          `json:"name,omitempty"`
	CronExpr        *string          `json:"cron_expr,omitempty"`
	DurationMinutes *int             `json:"duration_min,omitempty"`
	MaxRetries      *int             `json:"max_retries,omitempty"`
	Schedules       *[]ScheduleEntry `json:"schedules,omitempty"`
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

// GenerateCronForEntry generates a cron expression from a ScheduleEntry's Days and StartTime.
// Example: Days=[1,3,5], StartTime="20:30" => "30 20 * * 1,3,5"
func GenerateCronForEntry(entry ScheduleEntry) (string, error) {
	parts := timeRegex.FindStringSubmatch(entry.StartTime)
	if parts == nil {
		return "", fmt.Errorf("invalid start_time %q: must be HH:MM (24-hour)", entry.StartTime)
	}
	hour := parts[1]
	minute := parts[2]

	if len(entry.Days) == 0 {
		return "", fmt.Errorf("days must not be empty")
	}

	// Validate and deduplicate days
	seen := make(map[int]bool)
	var days []int
	for _, d := range entry.Days {
		if d < 0 || d > 6 {
			return "", fmt.Errorf("invalid day %d: must be 0-6 (0=Sun)", d)
		}
		if !seen[d] {
			seen[d] = true
			days = append(days, d)
		}
	}
	sort.Ints(days)

	var dayStrs []string
	for _, d := range days {
		dayStrs = append(dayStrs, fmt.Sprintf("%d", d))
	}

	expr := fmt.Sprintf("%s %s * * %s", minute, hour, strings.Join(dayStrs, ","))

	// Validate the generated expression
	if _, err := cronParser.Parse(expr); err != nil {
		return "", fmt.Errorf("generated cron expression is invalid: %w", err)
	}

	return expr, nil
}

// Validate checks the schedule entry and auto-generates its CronExpr.
func (e *ScheduleEntry) Validate() error {
	if len(e.Days) == 0 {
		return fmt.Errorf("days must not be empty")
	}
	seen := make(map[int]bool)
	for _, d := range e.Days {
		if d < 0 || d > 6 {
			return fmt.Errorf("invalid day %d: must be 0-6 (0=Sun)", d)
		}
		if seen[d] {
			return fmt.Errorf("duplicate day %d", d)
		}
		seen[d] = true
	}
	if !timeRegex.MatchString(e.StartTime) {
		return fmt.Errorf("invalid start_time %q: must be HH:MM (24-hour)", e.StartTime)
	}
	if e.DurationMin < 0 {
		return fmt.Errorf("duration_min must be >= 0")
	}

	cronExpr, err := GenerateCronForEntry(*e)
	if err != nil {
		return err
	}
	e.CronExpr = cronExpr
	return nil
}

// GetEffectiveDuration returns the duration for the given schedule index.
// Falls back to the legacy DurationMinutes when Schedules is empty.
func (t *ScheduleTask) GetEffectiveDuration(scheduleIdx int) int {
	if len(t.Schedules) > 0 && scheduleIdx >= 0 && scheduleIdx < len(t.Schedules) {
		return t.Schedules[scheduleIdx].DurationMin
	}
	return t.DurationMinutes
}

func (t *ScheduleTask) Validate() error {
	if t.RoomID == "" {
		return fmt.Errorf("room_id is required")
	}

	if len(t.Schedules) > 0 {
		// New multi-schedule mode: validate each entry
		for i := range t.Schedules {
			if err := t.Schedules[i].Validate(); err != nil {
				return fmt.Errorf("schedules[%d]: %w", i, err)
			}
		}
	} else {
		// Legacy single-cron mode
		if t.CronExpr == "" {
			return fmt.Errorf("cron_expr is required when schedules is empty")
		}
		if _, err := cronParser.Parse(t.CronExpr); err != nil {
			return fmt.Errorf("invalid cron_expr %q: %w", t.CronExpr, err)
		}
		if t.DurationMinutes < 0 {
			return fmt.Errorf("duration_min must be >= 0")
		}
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
	task := &ScheduleTask{
		Name:                r.Name,
		RoomID:              r.RoomID,
		RoomURL:             r.RoomURL,
		CronExpr:            r.CronExpr,
		DurationMinutes:     r.DurationMinutes,
		Enabled:             true,
		State:               StatePending,
		MaxRetries:          maxRetries,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
		Schedules:           r.Schedules,
		CurrentScheduleIdx:  -1,
		NextFireScheduleIdx: -1,
	}
	if task.Schedules == nil {
		task.Schedules = []ScheduleEntry{}
	}
	return task
}
