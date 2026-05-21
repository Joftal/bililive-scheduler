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

	"github.com/kira1928/bililive-scheduler/internal/api"
	"github.com/kira1928/bililive-scheduler/internal/client"
	"github.com/kira1928/bililive-scheduler/internal/config"
	"github.com/kira1928/bililive-scheduler/internal/cron"
	"github.com/kira1928/bililive-scheduler/internal/db"
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
	engine := cron.NewEngine(store, biliClient, 0) // default 15s interval

	// Start cron engine in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := engine.Start(ctx); err != nil {
			log.Printf("[cron] engine exited: %v", err)
		}
	}()

	// Init HTTP server
	apiServer := api.NewServer(store, biliClient, engine)
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
	if err := writePortFile(cfg.DBPath, actualPort); err != nil {
		log.Printf("[main] warning: failed to write port file: %v", err)
	}

	httpServer := &http.Server{Handler: router}

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

	// Graceful shutdown
	cancel() // stop cron engine

	// Stop active recordings before exit
	if err := stopActiveRecordings(ctx, store, biliClient); err != nil {
		log.Printf("[main] error stopping active recordings: %v", err)
	}

	httpServer.Close()
	log.Printf("[main] shutdown complete")
}

func writePortFile(dbPath string, port int) error {
	portFile := filepath.Join(filepath.Dir(dbPath), "scheduler.port")
	return os.WriteFile(portFile, []byte(fmt.Sprintf("%d", port)), 0644)
}

func stopActiveRecordings(ctx context.Context, store *db.Store, client *client.BiliAPI) error {
	tasks, err := store.GetRecordingTasks()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		log.Printf("[shutdown] stopping recording for task %d (room %s)", task.ID, task.RoomID)
		if err := client.StopRecording(ctx, task.RoomID); err != nil {
			log.Printf("[shutdown] stop task %d error: %v", task.ID, err)
		}
	}
	return nil
}
