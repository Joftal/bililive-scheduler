package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/kira1928/bililive-scheduler/internal/client"
	"github.com/kira1928/bililive-scheduler/internal/cron"
	"github.com/kira1928/bililive-scheduler/internal/db"
	"github.com/kira1928/bililive-scheduler/internal/model"
)

type Server struct {
	store  *db.Store
	client *client.BiliAPI
	engine *cron.Engine
}

func NewServer(store *db.Store, client *client.BiliAPI, engine *cron.Engine) *Server {
	return &Server{
		store:  store,
		client: client,
		engine: engine,
	}
}

func (s *Server) Router() *mux.Router {
	r := mux.NewRouter()
	r.Use(corsMiddleware)

	api := r.PathPrefix("/api").Subrouter()

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
			task.Schedules = newSchedules
			// Reset scheduling state when schedules change
			if task.State == model.StateWaiting {
				task.State = model.StatePending
				task.NextFireAt = nil
				task.NextFireScheduleIdx = -1
			}
		}
	}

	if err := task.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
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

	// Lock and check if task is recording
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

	// Stop recording before deleting
	if task.State == model.StateRecording {
		if stopErr := s.client.StopRecording(r.Context(), task.RoomID); stopErr != nil {
			writeError(w, http.StatusConflict, "cannot stop recording: "+stopErr.Error())
			return
		}
	}

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

	// Stop recording if active
	if task.State == model.StateRecording {
		if stopErr := s.client.StopRecording(r.Context(), task.RoomID); stopErr != nil {
			log.Printf("[api] disable task %d: stop recording error: %v", id, stopErr)
			task.State = model.StateError
			task.LastError = "stop failed: " + stopErr.Error()
		} else {
			task.State = model.StateCompleted
			task.LastError = "stopped by disable"
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
		writeError(w, http.StatusBadGateway, "failed to fetch rooms: "+err.Error())
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

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONWithStatus(w, http.StatusOK, v)
}

func writeJSONWithStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	if status >= 500 {
		log.Printf("[api] internal error: %s", msg)
		msg = "internal server error"
	}
	writeJSONWithStatus(w, status, map[string]string{"error": msg})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
