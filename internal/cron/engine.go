package cron

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/kira1928/bililive-scheduler/internal/client"
	"github.com/kira1928/bililive-scheduler/internal/db"
	"github.com/kira1928/bililive-scheduler/internal/model"
)

type Engine struct {
	store    *db.Store
	client   *client.BiliAPI
	interval time.Duration

	mu      sync.RWMutex
	running bool
	startAt time.Time
	cancel  context.CancelFunc
}

func NewEngine(store *db.Store, client *client.BiliAPI, interval time.Duration) *Engine {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &Engine{
		store:    store,
		client:   client,
		interval: interval,
	}
}

func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("engine already running")
	}
	ctx, e.cancel = context.WithCancel(ctx)
	e.running = true
	e.startAt = time.Now()
	e.mu.Unlock()

	log.Printf("[cron] engine started, interval=%s", e.interval)

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.mu.Lock()
			e.running = false
			e.mu.Unlock()
			log.Printf("[cron] engine stopped")
			return nil
		case <-ticker.C:
			if err := e.evaluate(ctx); err != nil {
				log.Printf("[cron] evaluate error: %v", err)
			}
		}
	}
}

func (e *Engine) Stop() {
	e.mu.RLock()
	cancel := e.cancel
	e.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

func (e *Engine) Uptime() time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.running {
		return 0
	}
	return time.Since(e.startAt)
}

func (e *Engine) evaluate(ctx context.Context) error {
	now := time.Now()

	// Phase 1: fire due tasks
	if err := e.fireDueTasks(ctx, now); err != nil {
		log.Printf("[cron] fireDueTasks error: %v", err)
	}

	// Phase 2: check active recordings
	if err := e.checkActiveRecordings(ctx, now); err != nil {
		log.Printf("[cron] checkActiveRecordings error: %v", err)
	}

	// Phase 3: reschedule completed/error tasks
	if err := e.rescheduleTasks(now); err != nil {
		log.Printf("[cron] rescheduleTasks error: %v", err)
	}

	return nil
}

func (e *Engine) fireDueTasks(ctx context.Context, now time.Time) error {
	tasks, err := e.store.GetDueTasks(now)
	if err != nil {
		return fmt.Errorf("get due tasks: %w", err)
	}

	for _, task := range tasks {
		if err := e.fireTask(ctx, task, now); err != nil {
			log.Printf("[cron] fire task %d (%s) error: %v", task.ID, task.Name, err)
			task.State = model.StateError
			task.LastError = err.Error()
			task.RetryCount++
			if err := e.store.Update(task); err != nil {
				log.Printf("[cron] update task %d error: %v", task.ID, err)
			}
		}
	}
	return nil
}

func (e *Engine) fireTask(ctx context.Context, task *model.ScheduleTask, now time.Time) error {
	// Check if room is live
	isLive, isRecording, err := e.client.GetRoomStatus(ctx, task.RoomID)
	if err != nil {
		return fmt.Errorf("get room status: %w", err)
	}

	if !isLive {
		log.Printf("[cron] task %d: room %s is not live, skipping", task.ID, task.RoomID)
		// Recompute next fire time
		next, err := NextAfter(task.CronExpr, now)
		if err != nil {
			return fmt.Errorf("compute next fire: %w", err)
		}
		task.NextFireAt = next
		task.State = model.StateWaiting
		return e.store.Update(task)
	}

	if isRecording {
		log.Printf("[cron] task %d: room %s is already recording, skipping", task.ID, task.RoomID)
		// Room is already recording, transition to recording state
		task.State = model.StateRecording
		task.CurrentLiveStart = &now
		task.LastError = ""
		return e.store.Update(task)
	}

	// Start recording
	log.Printf("[cron] task %d: starting recording for room %s", task.ID, task.RoomID)
	if err := e.client.StartRecording(ctx, task.RoomID); err != nil {
		return fmt.Errorf("start recording: %w", err)
	}

	// Record execution
	exec := &model.TaskExecution{
		TaskID:    task.ID,
		StartTime: now,
		State:     model.StateRecording,
	}
	if err := e.store.AddExecution(exec); err != nil {
		log.Printf("[cron] add execution for task %d error: %v", task.ID, err)
	}

	task.State = model.StateRecording
	task.CurrentLiveStart = &now
	task.LastError = ""
	task.RetryCount = 0
	return e.store.Update(task)
}

func (e *Engine) checkActiveRecordings(ctx context.Context, now time.Time) error {
	tasks, err := e.store.GetRecordingTasks()
	if err != nil {
		return fmt.Errorf("get recording tasks: %w", err)
	}

	for _, task := range tasks {
		if err := e.checkRecording(ctx, task, now); err != nil {
			log.Printf("[cron] check recording task %d error: %v", task.ID, err)
		}
	}
	return nil
}

func (e *Engine) checkRecording(ctx context.Context, task *model.ScheduleTask, now time.Time) error {
	// Check duration limit
	if task.DurationMinutes > 0 && task.CurrentLiveStart != nil {
		elapsed := now.Sub(*task.CurrentLiveStart)
		limit := time.Duration(task.DurationMinutes) * time.Minute
		if elapsed >= limit {
			log.Printf("[cron] task %d: duration limit reached (%s >= %s), stopping", task.ID, elapsed, limit)
			return e.stopTask(ctx, task, now, "duration limit reached")
		}
	}

	// Check if stream ended
	isLive, _, err := e.client.GetRoomStatus(ctx, task.RoomID)
	if err != nil {
		return fmt.Errorf("get room status: %w", err)
	}

	if !isLive {
		log.Printf("[cron] task %d: stream ended naturally", task.ID)
		return e.stopTask(ctx, task, now, "stream ended")
	}

	return nil
}

func (e *Engine) stopTask(ctx context.Context, task *model.ScheduleTask, now time.Time, reason string) error {
	// Stop recording
	if err := e.client.StopRecording(ctx, task.RoomID); err != nil {
		log.Printf("[cron] task %d: stop recording error: %v (continuing)", task.ID, err)
	}

	// Update execution history
	execs, err := e.store.GetExecutions(task.ID, 1)
	if err == nil && len(execs) > 0 {
		exec := execs[0]
		exec.EndTime = &now
		exec.State = model.StateCompleted
		exec.Error = reason
		if err := e.store.UpdateExecution(exec); err != nil {
			log.Printf("[cron] update execution for task %d error: %v", task.ID, err)
		}
	}

	task.State = model.StateCompleted
	task.LastError = reason
	return e.store.Update(task)
}

func (e *Engine) rescheduleTasks(now time.Time) error {
	tasks, err := e.store.List("", nil)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	for _, task := range tasks {
		if !task.Enabled {
			continue
		}

		switch task.State {
		case model.StateCompleted:
			// Reschedule for next cron occurrence
			next, err := NextAfter(task.CronExpr, now)
			if err != nil {
				log.Printf("[cron] task %d: invalid cron expr %q: %v", task.ID, task.CronExpr, err)
				task.State = model.StateError
				task.LastError = fmt.Sprintf("invalid cron: %v", err)
				if err := e.store.Update(task); err != nil {
					log.Printf("[cron] update task %d error: %v", task.ID, err)
				}
				continue
			}
			task.NextFireAt = next
			task.State = model.StateWaiting
			task.CurrentLiveStart = nil
			task.RetryCount = 0
			if err := e.store.Update(task); err != nil {
				log.Printf("[cron] update task %d error: %v", task.ID, err)
			}

		case model.StatePending:
			// First time evaluation
			next, err := NextAfter(task.CronExpr, now)
			if err != nil {
				task.State = model.StateError
				task.LastError = fmt.Sprintf("invalid cron: %v", err)
				if err := e.store.Update(task); err != nil {
					log.Printf("[cron] update task %d error: %v", task.ID, err)
				}
				continue
			}
			task.NextFireAt = next
			task.State = model.StateWaiting
			if err := e.store.Update(task); err != nil {
				log.Printf("[cron] update task %d error: %v", task.ID, err)
			}

		case model.StateError:
			// Retry with backoff if under max retries
			if task.RetryCount < task.MaxRetries {
				backoff := time.Duration(1<<uint(task.RetryCount)) * time.Minute
				if backoff > 15*time.Minute {
					backoff = 15 * time.Minute
				}
				next := now.Add(backoff)
				task.NextFireAt = &next
				task.State = model.StateWaiting
				if err := e.store.Update(task); err != nil {
					log.Printf("[cron] update task %d error: %v", task.ID, err)
				}
			}
		}
	}
	return nil
}
