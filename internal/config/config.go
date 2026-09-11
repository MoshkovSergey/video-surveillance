package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Config хранит конфигурацию приложения.
type Config struct {
	Environment     string
	HTTPAddr        string
	LogLevel        string
	DatabaseURL     string
	MediaMTXAPIURL  string
	ShutdownTimeout time.Duration
}

// Load читает конфигурацию из переменных окружения.
func Load() (Config, error) {
	cfg := Config{
		Environment:     getEnv("ENVIRONMENT", "development"),
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		MediaMTXAPIURL:  getEnv("MEDIAMTX_API_URL", "http://localhost:9997"),
		ShutdownTimeout: 10 * time.Second,
	}

	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must not be empty")
	}

	if strings.TrimSpace(cfg.MediaMTXAPIURL) == "" {
		return Config{}, fmt.Errorf("MEDIAMTX_API_URL must not be empty")
	}

	return cfg, nil
}

// ParseLogLevel преобразует строковое значение уровня логирования в slog.Level.
func ParseLogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); strings.TrimSpace(value) != "" {
		return value
	}

	return fallback
}
