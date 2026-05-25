package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/kira1928/bililive-scheduler/internal/client"
	"github.com/kira1928/bililive-scheduler/internal/config"
	"github.com/kira1928/bililive-scheduler/internal/cron"
	"github.com/kira1928/bililive-scheduler/internal/db"
	"github.com/kira1928/bililive-scheduler/internal/model"
	"golang.org/x/time/rate"
)

type Server struct {
	store  *db.Store
	client *client.BiliAPI
	engine *cron.Engine
	cfg    *config.Config
}

func NewServer(store *db.Store, client *client.BiliAPI, engine *cron.Engine, cfg *config.Config) *Server {
	return &Server{
		store:  store,
		client: client,
		engine: engine,
		cfg:    cfg,
	}
}

func (s *Server) Router() *mux.Router {
	r := mux.NewRouter()
	r.Use(securityHeadersMiddleware)
	r.Use(s.corsMiddleware)

	api := r.PathPrefix("/api").Subrouter()
	if s.cfg.APIKey != "" {
		api.Use(s.authMiddleware)
	}
	if s.cfg.RateLimit > 0 {
		api.Use(s.rateLimitMiddleware)
	}

	api.HandleFunc("/tasks", s.listTasks).Methods("GET")
	api.HandleFunc("/tasks", s.createTask).Methods("POST")
	api.HandleFunc("/tasks/{id:[0-9]+}", s.getTask).Methods("GET")
	api.HandleFunc("/tasks/{id:[0-9]+}", s.updateTask).Methods("PUT")
	api.HandleFunc("/tasks/{id:[0-9]+}", s.deleteTask).Methods("DELETE")
	api.HandleFunc("/tasks/{id:[0-9]+}/enable", s.enableTask).Methods("POST")
	api.HandleFunc("/tasks/{id:[0-9]+}/disable", s.disableTask).Methods("POST")
	api.HandleFunc("/tasks/{id:[0-9]+}/retry", s.retryTask).Methods("POST")
	api.HandleFunc("/tasks/{id:[0-9]+}/history", s.taskHistory).Methods("GET")

	api.HandleFunc("/status", s.schedulerStatus).Methods("GET")
	api.HandleFunc("/rooms", s.listRooms).Methods("GET")
	api.HandleFunc("/config", s.getConfig).Methods("GET")
	api.HandleFunc("/config", s.updateConfig).Methods("PUT")

	r.HandleFunc("/health", s.health).Methods("GET")
	return r
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	var enabled *bool
	if e := r.URL.Query().Get("enabled"); e != "" {
		b := e == "true" || e == "1"
		enabled = &b
	}

	tasks, err := s.store.List(state, enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []*model.ScheduleTask{}
	}
	writeJSON(w, tasks)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	var req model.CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	task := req.ToTask()
	if err := task.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if conflict := s.findScheduleConflict(task.RoomID, task.Schedules, 0); conflict != "" {
		writeError(w, http.StatusConflict, conflict)
		return
	}

	// Compute initial NextFireAt so the task doesn't fire immediately
	next, schedIdx, err := cron.NextScheduleAfter(task.Schedules, time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to compute next fire time: "+err.Error())
		return
	}
	task.NextFireAt = next
	task.NextFireScheduleIdx = schedIdx
	task.State = model.StateWaiting

	if err := s.store.Create(task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSONWithStatus(w, http.StatusCreated, task)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	task, err := s.store.Get(id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, task)
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req model.UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Lock to prevent race with cron engine
	s.store.TaskMu.Lock()
	defer s.store.TaskMu.Unlock()

	task, err := s.store.Get(id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	if req.Name != nil {
		task.Name = *req.Name
	}
	if req.MaxRetries != nil {
		task.MaxRetries = *req.MaxRetries
	}
	if req.Schedules != nil {
		newSchedules := *req.Schedules
		if newSchedules == nil {
			newSchedules = []model.ScheduleEntry{}
		}
		if len(newSchedules) > 0 {
			// Cannot modify schedules while recording (CurrentScheduleIdx would become invalid)
			if task.State == model.StateRecording {
				writeError(w, http.StatusConflict, "cannot modify schedules while task is recording")
				return
			}
			task.Schedules = newSchedules
			if task.State == model.StateWaiting {
				task.State = model.StatePending
				task.NextFireAt = nil
				task.NextFireScheduleIdx = -1
				task.RetryCount = 0
			}
		}
	}

	if err := task.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Schedules != nil {
		if conflict := s.findScheduleConflict(task.RoomID, task.Schedules, task.ID); conflict != "" {
			writeError(w, http.StatusConflict, conflict)
			return
		}
	}

	if err := s.store.Update(task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, task)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	// Phase 1: check task state under lock
	s.store.TaskMu.Lock()
	task, err := s.store.Get(id)
	if err != nil {
		s.store.TaskMu.Unlock()
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	needStop := task.State == model.StateRecording
	stopRoomID := task.RoomID
	s.store.TaskMu.Unlock()

	// Phase 2: stop recording outside lock (HTTP call, may be slow)
	if needStop {
		if stopErr := s.client.StopRecording(r.Context(), stopRoomID); stopErr != nil {
			log.Printf("[api] delete task %d: stop recording error: %v", id, stopErr)
			writeError(w, http.StatusConflict, "cannot stop recording")
			return
		}
	}

	// Phase 3: delete under lock
	s.store.TaskMu.Lock()
	defer s.store.TaskMu.Unlock()

	if err := s.store.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) enableTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	s.store.TaskMu.Lock()
	defer s.store.TaskMu.Unlock()

	task, err := s.store.Get(id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	task.Enabled = true
	// Reset error state so the engine can re-evaluate
	if task.State == model.StateError {
		task.State = model.StatePending
		task.RetryCount = 0
		task.LastError = ""
		task.NextFireAt = nil
		task.NextFireScheduleIdx = -1
		task.CurrentScheduleIdx = -1
	}

	if err := s.store.Update(task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, task)
}

func (s *Server) disableTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	// Phase 1: check task state under lock
	s.store.TaskMu.Lock()
	task, err := s.store.Get(id)
	if err != nil {
		s.store.TaskMu.Unlock()
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	needStop := task.State == model.StateRecording
	stopRoomID := task.RoomID
	s.store.TaskMu.Unlock()

	// Phase 2: stop recording outside lock (HTTP call, may be slow)
	var stopErr error
	if needStop {
		stopErr = s.client.StopRecording(r.Context(), stopRoomID)
	}

	// Phase 3: apply state changes under lock
	s.store.TaskMu.Lock()
	defer s.store.TaskMu.Unlock()

	// Re-get task in case it changed
	task, err = s.store.Get(id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	if needStop {
		if stopErr != nil {
			log.Printf("[api] disable task %d: stop recording error: %v", id, stopErr)
			task.State = model.StateError
			task.LastError = "stop failed"
		} else {
			task.State = model.StateCompleted
			task.LastError = "stopped by disable"
			now := time.Now()
			execs, _ := s.store.GetExecutions(task.ID, 1)
			if len(execs) > 0 && execs[0].State == model.StateRecording {
				exec := execs[0]
				exec.EndTime = &now
				exec.State = model.StateCompleted
				exec.Error = "stopped by disable"
				if err := s.store.UpdateExecution(exec); err != nil {
					log.Printf("[api] update execution for task %d error: %v", task.ID, err)
				}
			}
		}
	}

	task.Enabled = false
	task.NextFireAt = nil

	if err := s.store.Update(task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, task)
}

func (s *Server) retryTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	s.store.TaskMu.Lock()
	defer s.store.TaskMu.Unlock()

	task, err := s.store.Get(id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	if task.State != model.StateError {
		writeError(w, http.StatusBadRequest, "task is not in error state")
		return
	}

	task.State = model.StatePending
	task.RetryCount = 0
	task.LastError = ""
	task.NextFireAt = nil
	task.NextFireScheduleIdx = -1
	task.CurrentScheduleIdx = -1
	if err := s.store.Update(task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, task)
}

func (s *Server) taskHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 500 {
		limit = 500
	}

	execs, err := s.store.GetExecutions(id, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if execs == nil {
		execs = []*model.TaskExecution{}
	}
	writeJSON(w, execs)
}

func (s *Server) schedulerStatus(w http.ResponseWriter, r *http.Request) {
	total, recording, waiting, errored, err := s.store.GetCounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	biliReachable := s.client.HealthCheck(r.Context()) == nil

	status := model.SchedulerStatus{
		Running:          s.engine.IsRunning(),
		TotalTasks:       total,
		ActiveRecordings: recording,
		WaitingTasks:     waiting,
		ErrorTasks:       errored,
		BiliAPIReachable: biliReachable,
		Uptime:           s.engine.Uptime().Round(time.Second).String(),
	}
	writeJSON(w, status)
}

func (s *Server) listRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := s.client.GetRooms(r.Context())
	if err != nil {
		log.Printf("[api] listRooms upstream error: %v", err)
		writeError(w, http.StatusBadGateway, "failed to fetch rooms from upstream")
		return
	}
	writeJSON(w, rooms)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]bool{"ok": true})
}

func parseID(r *http.Request) (int64, error) {
	vars := mux.Vars(r)
	return strconv.ParseInt(vars["id"], 10, 64)
}

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	interval := s.engine.GetInterval()
	writeJSON(w, map[string]any{
		"tick_interval": interval,
	})
}

func (s *Server) updateConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		TickInterval *int `json:"tick_interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.TickInterval != nil {
		val := *req.TickInterval
		if val < 5 || val > 300 {
			writeError(w, http.StatusBadRequest, "tick_interval must be between 5 and 300")
			return
		}
		s.engine.SetInterval(val)
		if err := s.store.SetConfig("tick_interval", fmt.Sprintf("%d", val)); err != nil {
			log.Printf("[api] save tick_interval config error: %v", err)
		}
	}

	s.getConfig(w, r)
}

// findScheduleConflict checks if any existing task for the same room has overlapping schedules.
// excludeID is the task ID to skip (0 for create, task.ID for update).
// Returns a human-readable conflict message, or "" if no conflict.
func (s *Server) findScheduleConflict(roomID string, schedules []model.ScheduleEntry, excludeID int64) string {
	existing, err := s.store.List("", nil)
	if err != nil {
		return "" // can't check, allow through
	}

	for _, other := range existing {
		if other.ID == excludeID || other.RoomID != roomID {
			continue
		}
		for _, newEntry := range schedules {
			if newEntry.StartTime == "" {
				continue
			}
			for _, oldEntry := range other.Schedules {
				if oldEntry.StartTime == "" {
					continue
				}
				if newEntry.StartTime != oldEntry.StartTime {
					continue
				}
				overlap := dayOverlap(newEntry.Days, oldEntry.Days)
				if len(overlap) > 0 {
					dayNames := make([]string, len(overlap))
					isoDayNames := []string{"", "一", "二", "三", "四", "五", "六", "日"}
					for i, d := range overlap {
						dayNames[i] = isoDayNames[d]
					}
					return fmt.Sprintf("与任务 #%d 在周%s的 %s 时间冲突",
						other.ID, strings.Join(dayNames, "、"), newEntry.StartTime)
				}
			}
		}
	}
	return ""
}

func dayOverlap(a, b []int) []int {
	set := make(map[int]bool)
	for _, d := range a {
		set[d] = true
	}
	var result []int
	for _, d := range b {
		if set[d] {
			result = append(result, d)
		}
	}
	return result
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONWithStatus(w, http.StatusOK, v)
}

func writeJSONWithStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	if status >= 400 {
		log.Printf("[api] error (status %d): %s", status, msg)
		msg = sanitizeErrorMessage(msg)
	}
	writeJSONWithStatus(w, status, map[string]string{"error": msg})
}

var urlPrefixRegex = regexp.MustCompile(`https?://[^\s/]+`)

func sanitizeErrorMessage(msg string) string {
	return urlPrefixRegex.ReplaceAllString(msg, "upstream")
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	allowed := parseAllowedOrigins(s.cfg.AllowedOrigins)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if allowed["*"] {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
		} else if allowed["*"] {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func parseAllowedOrigins(raw string) map[string]bool {
	m := make(map[string]bool)
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			m[o] = true
		}
	}
	return m
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != s.cfg.APIKey {
			writeJSONWithStatus(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type rateLimitStore struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiter
	rate     rate.Limit
	burst    int
}

func newRateLimitStore(rps float64, burst int) *rateLimitStore {
	return &rateLimitStore{
		limiters: make(map[string]*ipLimiter),
		rate:     rate.Limit(rps),
		burst:    burst,
	}
}

func (s *rateLimitStore) getLimiter(ip string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	if l, ok := s.limiters[ip]; ok {
		l.lastSeen = time.Now()
		return l.limiter
	}
	limiter := rate.NewLimiter(s.rate, s.burst)
	s.limiters[ip] = &ipLimiter{limiter: limiter, lastSeen: time.Now()}
	return limiter
}

func (s *rateLimitStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ip, l := range s.limiters {
		if time.Since(l.lastSeen) > 3*time.Minute {
			delete(s.limiters, ip)
		}
	}
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	// Lazy init: create store once on first use
	var once sync.Once
	var store *rateLimitStore

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			store = newRateLimitStore(s.cfg.RateLimit, s.cfg.RateBurst)
			go func() {
				ticker := time.NewTicker(time.Minute)
				defer ticker.Stop()
				for range ticker.C {
					store.cleanup()
				}
			}()
		})

		ip := extractIP(r)
		if !store.getLimiter(ip).Allow() {
			writeJSONWithStatus(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "0")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if strings.HasPrefix(r.URL.Path, "/api") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
