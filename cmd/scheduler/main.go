package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kira1928/bililive-scheduler/internal/api"
	"github.com/kira1928/bililive-scheduler/internal/client"
	"github.com/kira1928/bililive-scheduler/internal/config"
	"github.com/kira1928/bililive-scheduler/internal/cron"
	"github.com/kira1928/bililive-scheduler/internal/db"
	"github.com/kira1928/bililive-scheduler/internal/model"
	"github.com/kira1928/bililive-scheduler/internal/webui"
)

func main() {
	cfg := config.Parse()

	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	log.Printf("[main] bililive-scheduler %s starting...", cfg.Version)
	log.Printf("[main] api-url=%s db-path=%s port=%d", cfg.APIURL, cfg.DBPath, cfg.Port)

	// Init database
	database, err := db.Init(cfg.DBPath)
	if err != nil {
		log.Fatalf("init database: %v", err)
	}
	defer database.Close()
	store := db.NewStore(database)

	// Init bililive-go API client
	biliClient := client.NewBiliAPI(cfg.APIURL)

	// Init cron engine
	tickInterval := 15 // default
	if saved, err := store.GetConfig("tick_interval", "15"); err == nil {
		fmt.Sscanf(saved, "%d", &tickInterval)
	}
	engine := cron.NewEngine(store, biliClient, tickInterval)
	log.Printf("[main] cron engine tick interval: %ds", tickInterval)

	// Start cron engine in background
	engineCtx, engineCancel := context.WithCancel(context.Background())
	defer engineCancel()

	go func() {
		if err := engine.Start(engineCtx); err != nil {
			log.Printf("[cron] engine exited: %v", err)
		}
	}()

	// Init HTTP server
	apiServer := api.NewServer(store, biliClient, engine, cfg)
	router := apiServer.Router()

	// Serve Web UI at root (catch-all, must be registered last)
	router.PathPrefix("/").Handler(webui.Handler())

	// Start HTTP server
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	log.Printf("[main] listening on port %d", actualPort)

	// Write port file for parent process discovery
	portFile := filepath.Join(filepath.Dir(cfg.DBPath), "scheduler.port")
	if err := os.WriteFile(portFile, []byte(fmt.Sprintf("%d", actualPort)), 0644); err != nil {
		log.Printf("[main] warning: failed to write port file: %v", err)
	}

	httpServer := &http.Server{
		Handler:           router,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("[main] received signal %v, shutting down...", sig)

	// Stop cron engine first
	engineCancel()

	// Stop active recordings using a FRESH context (the engine context is already cancelled)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := stopActiveRecordings(shutdownCtx, store, biliClient); err != nil {
		log.Printf("[main] error stopping active recordings: %v", err)
	}

	// Graceful HTTP shutdown (waits for in-flight requests)
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("[main] HTTP shutdown error: %v", err)
	}

	// Clean up port file
	os.Remove(portFile)

	log.Printf("[main] shutdown complete")
}

func stopActiveRecordings(ctx context.Context, store *db.Store, client *client.BiliAPI) error {
	tasks, err := store.GetRecordingTasks()
	if err != nil {
		return err
	}
	now := time.Now()
	for _, task := range tasks {
		log.Printf("[shutdown] stopping recording for task %d (room %s)", task.ID, task.RoomID)
		if err := client.StopRecording(ctx, task.RoomID); err != nil {
			log.Printf("[shutdown] stop task %d error: %v", task.ID, err)
		}
		// Update DB state
		store.TaskMu.Lock()
		task.State = model.StateCompleted
		task.CurrentScheduleIdx = -1
		if updateErr := store.Update(task); updateErr != nil {
			log.Printf("[shutdown] update task %d error: %v", task.ID, updateErr)
		}
		// Update execution history
		execs, _ := store.GetExecutions(task.ID, 1)
		if len(execs) > 0 && execs[0].State == model.StateRecording {
			exec := execs[0]
			exec.EndTime = &now
			exec.State = model.StateCompleted
			exec.Error = "stopped by shutdown"
			if err := store.UpdateExecution(exec); err != nil {
				log.Printf("[shutdown] update execution for task %d error: %v", task.ID, err)
			}
		}
		store.TaskMu.Unlock()
	}
	return nil
}
