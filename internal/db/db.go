package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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
			cron_expr       TEXT NOT NULL,
			duration_min    INTEGER NOT NULL DEFAULT 0,
			enabled         INTEGER NOT NULL DEFAULT 1,
			state           TEXT NOT NULL DEFAULT 'pending',
			next_fire_at    TIMESTAMP,
			current_live_start TIMESTAMP,
			last_error      TEXT,
			retry_count     INTEGER NOT NULL DEFAULT 0,
			max_retries     INTEGER NOT NULL DEFAULT 3,
			created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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
	return nil
}
