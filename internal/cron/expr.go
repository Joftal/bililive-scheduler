package cron

import (
	"fmt"
	"time"

	"github.com/kira1928/bililive-scheduler/internal/model"
)

// NextScheduleAfter finds the earliest next fire time across all schedule entries.
// Returns the fire time and the index of the entry that produces it.
func NextScheduleAfter(entries []model.ScheduleEntry, from time.Time) (*time.Time, int, error) {
	if len(entries) == 0 {
		return nil, -1, fmt.Errorf("no schedule entries")
	}

	var bestNext *time.Time
	bestIdx := -1

	for i, entry := range entries {
		if entry.CronExpr == "" {
			continue
		}
		schedule, err := model.CronParser.Parse(entry.CronExpr)
		if err != nil {
			continue
		}
		next := schedule.Next(from)
		if bestNext == nil || next.Before(*bestNext) {
			bestNext = &next
			bestIdx = i
		}
	}

	if bestNext == nil {
		return nil, -1, fmt.Errorf("no valid schedule entries with parseable cron expressions")
	}
	return bestNext, bestIdx, nil
}
