package cron

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kira1928/bililive-scheduler/internal/client"
	"github.com/kira1928/bililive-scheduler/internal/db"
	"github.com/kira1928/bililive-scheduler/internal/model"
)

type Engine struct {
	store    *db.Store
	client   *client.BiliAPI
	interval atomic.Int32 // seconds, hot-reloadable

	mu          sync.RWMutex
	running     bool
	startAt     time.Time
	cancel      context.CancelFunc
	lastCleanup time.Time
}

func NewEngine(store *db.Store, client *client.BiliAPI, interval int) *Engine {
	if interval <= 0 {
		interval = 15
	}
	e := &Engine{
		store:       store,
		client:      client,
		lastCleanup: time.Now(),
	}
	e.interval.Store(int32(interval))
	return e
}

// SetInterval updates the tick interval in seconds (hot-reloadable).
func (e *Engine) SetInterval(seconds int) {
	if seconds < 5 {
		seconds = 5
	}
	if seconds > 300 {
		seconds = 300
	}
	e.interval.Store(int32(seconds))
}

// GetInterval returns the current tick interval in seconds.
func (e *Engine) GetInterval() int {
	return int(e.interval.Load())
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

	log.Printf("[cron] engine started, interval=%ds", e.interval.Load())

	for {
		d := time.Duration(e.interval.Load()) * time.Second
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			e.mu.Lock()
			e.running = false
			e.mu.Unlock()
			log.Printf("[cron] engine stopped")
			return nil
		case <-t.C:
			e.evaluate(ctx)
		}
	}
}

// Stop cancels the engine context, causing the tick loop to exit.
// Active recordings are stopped by the caller (main.go) after this returns.
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

func (e *Engine) evaluate(ctx context.Context) {
	now := time.Now()

	// Phase 1: fire due tasks
	e.fireDueTasks(ctx, now)

	// Phase 2: check active recordings
	e.checkActiveRecordings(ctx, now)

	// Phase 3: reschedule completed/error tasks
	e.rescheduleTasks(now)

	// Phase 4: daily cleanup of old execution history
	if now.Sub(e.lastCleanup) > 24*time.Hour {
		if deleted, err := e.store.CleanupExecutions(60); err != nil {
			log.Printf("[cron] cleanup executions error: %v", err)
		} else if deleted > 0 {
			log.Printf("[cron] cleaned up %d old execution records", deleted)
		}
		e.lastCleanup = now
	}
}

func (e *Engine) fireDueTasks(ctx context.Context, now time.Time) {
	tasks, err := e.store.GetDueTasks(now)
	if err != nil {
		log.Printf("[cron] get due tasks error: %v", err)
		return
	}

	for _, task := range tasks {
		if err := e.fireTask(ctx, task, now); err != nil {
			log.Printf("[cron] fire task %d (%s) error: %v", task.ID, task.Name, err)
			e.store.TaskMu.Lock()
			fresh, getErr := e.store.Get(task.ID)
			if getErr != nil {
				e.store.TaskMu.Unlock()
				continue // task was deleted
			}
			fresh.State = model.StateError
			fresh.LastError = err.Error()
			fresh.RetryCount++
			if updateErr := e.store.Update(fresh); updateErr != nil {
				log.Printf("[cron] update task %d error: %v", task.ID, updateErr)
			}
			e.store.TaskMu.Unlock()
		}
	}
}

func (e *Engine) fireTask(ctx context.Context, task *model.ScheduleTask, now time.Time) error {
	// Check if room is live (HTTP call, do outside lock)
	isLive, isRecording, err := e.client.GetRoomStatus(ctx, task.RoomID)
	if err != nil {
		if errors.Is(err, client.ErrRoomNotFound) {
			// Room was deleted in bililive-go — remove the task
			log.Printf("[cron] task %d: room %s not found, deleting task", task.ID, task.RoomID)
			e.store.TaskMu.Lock()
			deleteErr := e.store.Delete(task.ID)
			e.store.TaskMu.Unlock()
			if deleteErr != nil {
				log.Printf("[cron] delete task %d error: %v", task.ID, deleteErr)
			}
			return nil
		}
		return fmt.Errorf("get room status: %w", err)
	}

	e.store.TaskMu.Lock()
	defer e.store.TaskMu.Unlock()

	// Re-read task from DB to confirm it's still in a fireable state
	// (could have been deleted, disabled, or modified by API handler)
	fresh, err := e.store.Get(task.ID)
	if err != nil {
		return nil // task was deleted, nothing to do
	}
	if !fresh.Enabled {
		return nil // task was disabled, skip
	}
	if fresh.State != model.StatePending && fresh.State != model.StateWaiting {
		return nil // task state changed, skip
	}

	// Don't fire a task that isn't actually due yet.
	// Defense-in-depth: GetDueTasks already filters by state and NextFireAt,
	// but this catches edge cases (e.g. stale data from the query snapshot).
	if fresh.NextFireAt == nil || now.Before(*fresh.NextFireAt) {
		return nil // not yet due
	}

	// If NextFireAt is significantly in the past (more than one tick interval),
	// the service was likely down and missed the trigger. Reschedule to the
	// next future fire time instead of catch-up firing.
	tickDur := time.Duration(e.interval.Load()) * time.Second
	if overdue := now.Sub(*fresh.NextFireAt); overdue > tickDur {
		log.Printf("[cron] task %d: NextFireAt %s is %s in the past, rescheduling",
			fresh.ID, fresh.NextFireAt.Format("15:04:05"), overdue.Round(time.Second))
		next, schedIdx, err := nextFireTime(fresh, now)
		if err != nil {
			return fmt.Errorf("compute next fire for overdue task: %w", err)
		}
		fresh.NextFireAt = next
		fresh.NextFireScheduleIdx = schedIdx
		fresh.State = model.StateWaiting
		return e.store.Update(fresh)
	}

	if !isLive {
		monitorMin := fresh.GetEffectiveMonitorMin(fresh.NextFireScheduleIdx)
		if monitorMin > 0 {
			// Monitoring mode: keep checking within the monitoring window
			if fresh.MonitorUntil == nil {
				// First check — set monitoring window from the scheduled fire time
				monitorEnd := fresh.NextFireAt.Add(time.Duration(monitorMin) * time.Minute)
				fresh.MonitorUntil = &monitorEnd
			}
			if now.Before(*fresh.MonitorUntil) {
				// Still within monitoring window — check again next tick
				log.Printf("[cron] task %d: room %s not live, monitoring until %s",
					task.ID, task.RoomID, fresh.MonitorUntil.Format("15:04:05"))
				tickDur := time.Duration(e.interval.Load()) * time.Second
				nextTick := now.Add(tickDur)
				fresh.NextFireAt = &nextTick
				fresh.State = model.StateWaiting
				return e.store.Update(fresh)
			}
			// Monitoring window expired — give up and reschedule
			log.Printf("[cron] task %d: room %s monitoring window expired", task.ID, task.RoomID)
			fresh.MonitorUntil = nil
		} else {
			log.Printf("[cron] task %d: room %s is not live, skipping", task.ID, task.RoomID)
		}
		next, schedIdx, err := nextFireTime(fresh, now)
		if err != nil {
			return fmt.Errorf("compute next fire: %w", err)
		}
		fresh.NextFireAt = next
		fresh.NextFireScheduleIdx = schedIdx
		fresh.State = model.StateWaiting
		return e.store.Update(fresh)
	}

	if isRecording {
		log.Printf("[cron] task %d: room %s is already recording", task.ID, task.RoomID)
		fresh.State = model.StateRecording
		fresh.CurrentLiveStart = &now
		fresh.LastError = ""
		// Keep MonitorUntil — used to detect reconnection window if stream drops
		if len(fresh.Schedules) > 0 {
			fresh.CurrentScheduleIdx = fresh.NextFireScheduleIdx
		}
		// Create execution record if none exists
		execs, _ := e.store.GetExecutions(fresh.ID, 1)
		if len(execs) == 0 || execs[0].State != model.StateRecording {
			exec := &model.TaskExecution{
				TaskID:    fresh.ID,
				StartTime: now,
				State:     model.StateRecording,
			}
			if err := e.store.AddExecution(exec); err != nil {
				log.Printf("[cron] add execution for task %d error: %v", fresh.ID, err)
			}
		}
		return e.store.Update(fresh)
	}

	// Start recording
	log.Printf("[cron] task %d: starting recording for room %s", task.ID, task.RoomID)
	if err := e.client.StartRecording(ctx, task.RoomID); err != nil {
		return fmt.Errorf("start recording: %w", err)
	}

	// Record execution
	exec := &model.TaskExecution{
		TaskID:    fresh.ID,
		StartTime: now,
		State:     model.StateRecording,
	}
	if err := e.store.AddExecution(exec); err != nil {
		log.Printf("[cron] add execution for task %d error: %v", fresh.ID, err)
	}

	fresh.State = model.StateRecording
	fresh.CurrentLiveStart = &now
	fresh.LastError = ""
	fresh.RetryCount = 0
	// Set or keep MonitorUntil for reconnection window if stream drops later
	if fresh.MonitorUntil == nil {
		if monitorMin := fresh.GetEffectiveMonitorMin(fresh.NextFireScheduleIdx); monitorMin > 0 {
			if fresh.NextFireAt != nil {
				monitorEnd := fresh.NextFireAt.Add(time.Duration(monitorMin) * time.Minute)
				fresh.MonitorUntil = &monitorEnd
			}
		}
	}
	// Set CurrentScheduleIdx from NextFireScheduleIdx
	if len(fresh.Schedules) > 0 {
		fresh.CurrentScheduleIdx = fresh.NextFireScheduleIdx
	}
	return e.store.Update(fresh)
}

func (e *Engine) checkActiveRecordings(ctx context.Context, now time.Time) {
	tasks, err := e.store.GetRecordingTasks()
	if err != nil {
		log.Printf("[cron] get recording tasks error: %v", err)
		return
	}

	for _, task := range tasks {
		if err := e.checkRecording(ctx, task, now); err != nil {
			log.Printf("[cron] check recording task %d error: %v", task.ID, err)
		}
	}
}

func (e *Engine) checkRecording(ctx context.Context, task *model.ScheduleTask, now time.Time) error {
	// If task is disabled but still recording, stop it
	if !task.Enabled {
		log.Printf("[cron] task %d: disabled but still recording, stopping", task.ID)
		return e.stopTask(ctx, task, now, "task disabled")
	}

	// Check duration limit under lock to avoid race with fireTask modifying CurrentScheduleIdx
	e.store.TaskMu.Lock()
	fresh, err := e.store.Get(task.ID)
	if err != nil {
		e.store.TaskMu.Unlock()
		return nil // task was deleted
	}
	duration := fresh.GetEffectiveDuration(fresh.CurrentScheduleIdx)
	limitReached := false
	if duration > 0 && fresh.CurrentLiveStart != nil {
		elapsed := now.Sub(*fresh.CurrentLiveStart)
		limit := time.Duration(duration) * time.Minute
		if elapsed >= limit {
			log.Printf("[cron] task %d: duration limit reached (%s >= %s), stopping", task.ID, elapsed, limit)
			limitReached = true
		}
	}
	e.store.TaskMu.Unlock()

	if limitReached {
		return e.stopTask(ctx, task, now, "duration limit reached")
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

	e.store.TaskMu.Lock()
	defer e.store.TaskMu.Unlock()

	// Re-read from DB to avoid stale data (API may have modified the task)
	fresh, err := e.store.Get(task.ID)
	if err != nil {
		return nil // task was deleted
	}

	// Update or create execution history
	isNormalEnd := reason == "stream ended"
	execs, err := e.store.GetExecutions(fresh.ID, 1)
	if err == nil && len(execs) > 0 && execs[0].State == model.StateRecording {
		exec := execs[0]
		exec.EndTime = &now
		exec.State = model.StateCompleted
		if !isNormalEnd {
			exec.Error = reason
		}
		if err := e.store.UpdateExecution(exec); err != nil {
			log.Printf("[cron] update execution for task %d error: %v", fresh.ID, err)
		}
	} else {
		// No active execution record — create one
		exec := &model.TaskExecution{
			TaskID:    fresh.ID,
			StartTime: now,
			EndTime:   &now,
			State:     model.StateCompleted,
		}
		if !isNormalEnd {
			exec.Error = reason
		}
		if err := e.store.AddExecution(exec); err != nil {
			log.Printf("[cron] add execution for task %d error: %v", fresh.ID, err)
		}
	}

	// If stream ended naturally and still within the monitoring window,
	// enter monitoring mode to wait for reconnection instead of completing.
	if isNormalEnd && fresh.MonitorUntil != nil && now.Before(*fresh.MonitorUntil) {
		log.Printf("[cron] task %d: room %s stream dropped, monitoring for reconnection until %s",
			fresh.ID, fresh.RoomID, fresh.MonitorUntil.Format("15:04:05"))
		tickDur := time.Duration(e.interval.Load()) * time.Second
		nextTick := now.Add(tickDur)
		fresh.NextFireAt = &nextTick
		fresh.State = model.StateWaiting
		fresh.CurrentScheduleIdx = -1
		return e.store.Update(fresh)
	}

	fresh.State = model.StateCompleted
	if !isNormalEnd {
		fresh.LastError = reason
	}
	fresh.CurrentScheduleIdx = -1
	fresh.MonitorUntil = nil
	return e.store.Update(fresh)
}

func (e *Engine) rescheduleTasks(now time.Time) {
	tasks, err := e.store.GetReschedulableTasks()
	if err != nil {
		log.Printf("[cron] get reschedulable tasks error: %v", err)
		return
	}

	for _, task := range tasks {
		e.store.TaskMu.Lock()
		// Re-read from DB to get fresh state (fireTask or API may have changed it)
		fresh, err := e.store.Get(task.ID)
		if err != nil {
			e.store.TaskMu.Unlock()
			continue // task was deleted
		}
		if !fresh.Enabled {
			e.store.TaskMu.Unlock()
			continue
		}
		e.rescheduleOneTask(fresh, now)
		e.store.TaskMu.Unlock()
	}
}

func (e *Engine) rescheduleOneTask(task *model.ScheduleTask, now time.Time) {
	switch task.State {
	case model.StateCompleted:
		next, schedIdx, err := nextFireTime(task, now)
		if err != nil {
			log.Printf("[cron] task %d: compute next fire: %v", task.ID, err)
			task.State = model.StateError
			task.LastError = fmt.Sprintf("invalid schedule: %v", err)
			if err := e.store.Update(task); err != nil {
				log.Printf("[cron] update task %d error: %v", task.ID, err)
			}
			return
		}
		task.NextFireAt = next
		task.NextFireScheduleIdx = schedIdx
		task.State = model.StateWaiting
		task.CurrentLiveStart = nil
		task.CurrentScheduleIdx = -1
		task.MonitorUntil = nil
		task.RetryCount = 0
		if err := e.store.Update(task); err != nil {
			log.Printf("[cron] update task %d error: %v", task.ID, err)
		}

	case model.StatePending:
		next, schedIdx, err := nextFireTime(task, now)
		if err != nil {
			task.State = model.StateError
			task.LastError = fmt.Sprintf("invalid schedule: %v", err)
			if err := e.store.Update(task); err != nil {
				log.Printf("[cron] update task %d error: %v", task.ID, err)
			}
			return
		}
		task.NextFireAt = next
		task.NextFireScheduleIdx = schedIdx
		task.State = model.StateWaiting
		if err := e.store.Update(task); err != nil {
			log.Printf("[cron] update task %d error: %v", task.ID, err)
		}

	case model.StateError:
		if task.RetryCount < task.MaxRetries {
			// Compute next fire time from schedules, then apply backoff if needed
			nextFromSchedule, schedIdx, schedErr := nextFireTime(task, now)
			retryCount := task.RetryCount
			if retryCount > 20 {
				retryCount = 20
			}
			backoff := time.Duration(1<<uint(retryCount)) * time.Minute
			if backoff > 15*time.Minute {
				backoff = 15 * time.Minute
			}
			backoffTime := now.Add(backoff)

			if schedErr == nil && nextFromSchedule.After(backoffTime) {
				// Schedule fire is later than backoff — use schedule time
				task.NextFireAt = nextFromSchedule
				task.NextFireScheduleIdx = schedIdx
			} else if schedErr == nil {
				// Backoff is later — use backoff time but remember the schedule idx
				task.NextFireAt = &backoffTime
				task.NextFireScheduleIdx = schedIdx
			} else {
				// No valid schedule — fall back to pure backoff
				task.NextFireAt = &backoffTime
				task.NextFireScheduleIdx = -1
			}
			task.State = model.StateWaiting
			if err := e.store.Update(task); err != nil {
				log.Printf("[cron] update task %d error: %v", task.ID, err)
			}
		}
	}
}

// nextFireTime computes the next fire time for a task across all schedule entries.
func nextFireTime(task *model.ScheduleTask, from time.Time) (*time.Time, int, error) {
	return NextScheduleAfter(task.Schedules, from)
}
