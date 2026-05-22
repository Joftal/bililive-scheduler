package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func Init(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS schedule_tasks (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT NOT NULL DEFAULT '',
			room_id         TEXT NOT NULL,
			room_url        TEXT NOT NULL DEFAULT '',
			enabled         INTEGER NOT NULL DEFAULT 1,
			state           TEXT NOT NULL DEFAULT 'pending',
			next_fire_at    TIMESTAMP,
			current_live_start TIMESTAMP,
			last_error      TEXT,
			retry_count     INTEGER NOT NULL DEFAULT 0,
			max_retries     INTEGER NOT NULL DEFAULT 3,
			created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			schedules       TEXT DEFAULT '[]',
			current_schedule_idx INTEGER DEFAULT -1,
			next_fire_schedule_idx INTEGER DEFAULT -1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_enabled ON schedule_tasks(enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_next_fire ON schedule_tasks(next_fire_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_state ON schedule_tasks(state)`,
		`CREATE TABLE IF NOT EXISTS task_executions (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id     INTEGER NOT NULL REFERENCES schedule_tasks(id) ON DELETE CASCADE,
			start_time  TIMESTAMP NOT NULL,
			end_time    TIMESTAMP,
			state       TEXT NOT NULL,
			error       TEXT DEFAULT '',
			created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_executions_task_id ON task_executions(task_id)`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("execute migration: %w", err)
		}
	}

	// Incremental migrations for existing databases
	alterMigrations := []string{
		`ALTER TABLE schedule_tasks ADD COLUMN schedules TEXT DEFAULT '[]'`,
		`ALTER TABLE schedule_tasks ADD COLUMN current_schedule_idx INTEGER DEFAULT -1`,
		`ALTER TABLE schedule_tasks ADD COLUMN next_fire_schedule_idx INTEGER DEFAULT -1`,
	}
	for _, m := range alterMigrations {
		if _, err := db.Exec(m); err != nil {
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("execute alter migration: %w", err)
			}
		}
	}

	// Remove deprecated columns (idempotent: ignore if already dropped)
	dropMigrations := []string{
		`ALTER TABLE schedule_tasks DROP COLUMN cron_expr`,
		`ALTER TABLE schedule_tasks DROP COLUMN duration_min`,
	}
	for _, m := range dropMigrations {
		if _, err := db.Exec(m); err != nil {
			if !isNoSuchColumnError(err) {
				return fmt.Errorf("execute drop migration: %w", err)
			}
		}
	}

	return nil
}

func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}

func isNoSuchColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such column") || strings.Contains(msg, "duplicate column name")
}
