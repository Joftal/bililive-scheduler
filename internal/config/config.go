package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Config struct {
	Port           int
	APIURL         string
	DBPath         string
	LogLevel       string
	Version        string
	APIKey         string
	AllowedOrigins string
	RateLimit      float64
	RateBurst      int
}

func Parse() *Config {
	cfg := &Config{}

	flag.IntVar(&cfg.Port, "port", 0, "HTTP server port (0 = random)")
	flag.StringVar(&cfg.APIURL, "api-url", "http://localhost:8080", "bililive-go API base URL")
	flag.StringVar(&cfg.DBPath, "db-path", "", "SQLite database path")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&cfg.Version, "version", "0.1.0", "Version string")
	flag.StringVar(&cfg.APIKey, "api-key", "", "API key for authentication (empty = disabled)")
	flag.StringVar(&cfg.AllowedOrigins, "allowed-origins", "*", "Comma-separated list of allowed CORS origins (\"*\" = allow all)")
	flag.Float64Var(&cfg.RateLimit, "rate-limit", 30, "Rate limit per IP (requests/second, 0 = disabled)")
	flag.IntVar(&cfg.RateBurst, "rate-burst", 60, "Rate limit burst size")
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
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("port must be 0-65535")
	}
	return nil
}

func defaultDBPath() string {
	var dir string
	switch runtime.GOOS {
	case "windows":
		dir = os.Getenv("APPDATA")
		if dir == "" {
			dir = os.Getenv("LOCALAPPDATA")
		}
		if dir == "" {
			dir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		dir = filepath.Join(dir, "bililive-go")
	case "darwin":
		home := os.Getenv("HOME")
		if home == "" {
			home = "/tmp"
		}
		dir = filepath.Join(home, "Library", "Application Support", "bililive-go")
	default:
		// Linux: honor XDG_CONFIG_HOME
		dir = os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			home := os.Getenv("HOME")
			if home == "" {
				home = "/tmp"
			}
			dir = filepath.Join(home, ".config")
		}
		dir = filepath.Join(dir, "bililive-go")
	}
	return filepath.Join(dir, "db", "scheduler.db")
}
