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

// ScheduleEntry represents a single time slot in a task.
type ScheduleEntry struct {
	Days        []int  `json:"days"`         // 1=Mon, 2=Tue, ..., 6=Sat, 7=Sun (ISO 8601)
	StartTime   string `json:"start_time"`   // "HH:MM" format, 24-hour
	DurationMin int    `json:"duration_min"` // 0 = until stream ends
	CronExpr    string `json:"cron_expr"`    // auto-generated, read-only for clients
}

type ScheduleTask struct {
	ID                  int64           `json:"id"`
	Name                string          `json:"name"`
	RoomID              string          `json:"room_id"`
	RoomURL             string          `json:"room_url"`
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
	Name       string          `json:"name"`
	RoomID     string          `json:"room_id"`
	RoomURL    string          `json:"room_url"`
	MaxRetries *int            `json:"max_retries"`
	Schedules  []ScheduleEntry `json:"schedules"`
}

type UpdateTaskRequest struct {
	Name       *string          `json:"name,omitempty"`
	MaxRetries *int             `json:"max_retries,omitempty"`
	Schedules  *[]ScheduleEntry `json:"schedules,omitempty"`
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
// Days are in ISO 8601 format (1=Mon..7=Sun), converted to cron format (0=Sun..6=Sat).
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

	seen := make(map[int]bool)
	var cronDays []int
	for _, d := range entry.Days {
		if d < 1 || d > 7 {
			return "", fmt.Errorf("invalid day %d: must be 1-7 (1=Mon, 7=Sun)", d)
		}
		cronDay := d % 7 // ISO 7 (Sun) → cron 0 (Sun), others stay the same
		if !seen[cronDay] {
			seen[cronDay] = true
			cronDays = append(cronDays, cronDay)
		}
	}
	sort.Ints(cronDays)

	var dayStrs []string
	for _, d := range cronDays {
		dayStrs = append(dayStrs, fmt.Sprintf("%d", d))
	}

	expr := fmt.Sprintf("%s %s * * %s", minute, hour, strings.Join(dayStrs, ","))

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
		if d < 1 || d > 7 {
			return fmt.Errorf("invalid day %d: must be 1-7 (1=Mon, 7=Sun)", d)
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
func (t *ScheduleTask) GetEffectiveDuration(scheduleIdx int) int {
	if scheduleIdx >= 0 && scheduleIdx < len(t.Schedules) {
		return t.Schedules[scheduleIdx].DurationMin
	}
	return 0
}

func (t *ScheduleTask) Validate() error {
	if t.RoomID == "" {
		return fmt.Errorf("room_id is required")
	}
	if len(t.Schedules) == 0 {
		return fmt.Errorf("at least one schedule entry is required")
	}
	for i := range t.Schedules {
		if err := t.Schedules[i].Validate(); err != nil {
			return fmt.Errorf("schedules[%d]: %w", i, err)
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
	maxRetries := 3
	if r.MaxRetries != nil {
		maxRetries = *r.MaxRetries
	}
	task := &ScheduleTask{
		Name:                r.Name,
		RoomID:              r.RoomID,
		RoomURL:             r.RoomURL,
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
