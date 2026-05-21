package cron

import (
	"fmt"
	"time"

	cronlib "github.com/robfig/cron/v3"
)

var parser = cronlib.NewParser(cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow)

func Parse(expr string) (cronlib.Schedule, error) {
	return parser.Parse(expr)
}

func NextN(expr string, n int, from time.Time) ([]time.Time, error) {
	schedule, err := Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}

	times := make([]time.Time, 0, n)
	t := from
	for i := 0; i < n; i++ {
		t = schedule.Next(t)
		times = append(times, t)
	}
	return times, nil
}

func NextAfter(expr string, from time.Time) (*time.Time, error) {
	schedule, err := Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	next := schedule.Next(from)
	return &next, nil
}

func Describe(expr string) string {
	schedule, err := Parse(expr)
	if err != nil {
		return expr
	}
	next := schedule.Next(time.Now())
	return fmt.Sprintf("%s (next: %s)", expr, next.Format("2006-01-02 15:04:05"))
}
