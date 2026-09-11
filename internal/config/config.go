package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
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
	StoragePath     string
	ShutdownTimeout time.Duration

	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	AdminUsername   string
	AdminPassword   string
}

// Load читает конфигурацию из переменных окружения.
func Load() (Config, error) {
	cfg := Config{
		Environment:     getEnv("ENVIRONMENT", "development"),
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		MediaMTXAPIURL:  getEnv("MEDIAMTX_API_URL", "http://127.0.0.1:9999"),
		StoragePath:     getEnv("STORAGE_PATH", "./storage"),
		ShutdownTimeout: 10 * time.Second,

		JWTSecret:       getEnv("JWT_SECRET", "dev-secret-change-me"),
		AccessTokenTTL:  time.Duration(getEnvInt("ACCESS_TOKEN_TTL_MINUTES", 15)) * time.Minute,
		RefreshTokenTTL: time.Duration(getEnvInt("REFRESH_TOKEN_TTL_HOURS", 24)) * time.Hour,
		AdminUsername:   getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:   getEnv("ADMIN_PASSWORD", "admin123"),
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

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if strings.TrimSpace(value) == "" {
		return fallback
	}

	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}