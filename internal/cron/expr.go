package cron

import (
	"fmt"
	"time"

	cronlib "github.com/robfig/cron/v3"
)

var Parser = cronlib.NewParser(cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow)

func Parse(expr string) (cronlib.Schedule, error) {
	return Parser.Parse(expr)
}

func NextAfter(expr string, from time.Time) (*time.Time, error) {
	schedule, err := Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	next := schedule.Next(from)
	return &next, nil
}
