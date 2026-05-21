package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Config struct {
	Port       int
	APIURL     string
	DBPath     string
	LogLevel   string
	Version    string
}

func Parse() *Config {
	cfg := &Config{}

	flag.IntVar(&cfg.Port, "port", 0, "HTTP server port (0 = random)")
	flag.StringVar(&cfg.APIURL, "api-url", "http://localhost:8080", "bililive-go API base URL")
	flag.StringVar(&cfg.DBPath, "db-path", "", "SQLite database path")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&cfg.Version, "version", "0.1.0", "Version string")
	flag.Parse()

	if cfg.DBPath == "" {
		cfg.DBPath = defaultDBPath()
	}

	return cfg
}

func (c *Config) Validate() error {
	if c.APIURL == "" {
		return fmt.Errorf("api-url is required")
	}
	return nil
}

func defaultDBPath() string {
	var dir string
	switch runtime.GOOS {
	case "windows":
		dir = os.Getenv("APPDATA")
		if dir == "" {
			dir = os.Getenv("USERPROFILE")
		}
		dir = filepath.Join(dir, "bililive-go")
	case "darwin":
		home := os.Getenv("HOME")
		dir = filepath.Join(home, "Library", "Application Support", "bililive-go")
	default:
		home := os.Getenv("HOME")
		dir = filepath.Join(home, ".config", "bililive-go")
	}
	return filepath.Join(dir, "db", "scheduler.db")
}
