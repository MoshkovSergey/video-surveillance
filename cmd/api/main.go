package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"gitverse.ru/cataclysm78/video-surveillance/internal/config"
	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	httpapi "gitverse.ru/cataclysm78/video-surveillance/internal/http"
	"gitverse.ru/cataclysm78/video-surveillance/internal/mediamtx"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

func main() {
	// Загружаем .env локально, если он есть.
	// В контейнере переменные окружения обычно передаются без .env файла.
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	logger.Info("database connection established")

	cameraRepo := postgres.NewCameraRepository(pool)
	media := mediamtx.NewClient(cfg.MediaMTXAPIURL)

	// Регистрируем в MediaMTX все активные камеры из базы данных.
	syncMediaPaths(ctx, cameraRepo, media, logger)

	handler := httpapi.NewHandler(pool, cameraRepo, media, logger)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("api server starting", "addr", cfg.HTTPAddr)

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api server stopped unexpectedly", "error", err)
			stop()
		}
	}()

	<-ctx.Done()

	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	logger.Info("server stopped")
}

// syncMediaPaths регистрирует пути MediaMTX для всех активных камер из базы данных.
func syncMediaPaths(ctx context.Context, repo *postgres.CameraRepository, media *mediamtx.Client, logger *slog.Logger) {
	// Сначала убеждаемся, что на указанном порту отвечает именно MediaMTX.
	if err := media.Ping(ctx); err != nil {
		logger.Error("mediamtx api is not available; stream paths will not be registered", "error", err)
		return
	}

	cams, err := repo.List(ctx)
	if err != nil {
		logger.Error("failed to list cameras for mediamtx sync", "error", err)
		return
	}

	for _, cam := range cams {
		if cam.Status != domain.CameraStatusEnabled {
			continue
		}

		if err := media.AddPath(ctx, "cam_"+cam.ID.String(), cam.RTSPUri); err != nil {
			logger.Error("failed to sync mediamtx path",
				"camera_id", cam.ID.String(),
				"error", err,
			)
		}
	}

	logger.Info("mediamtx paths synchronized", "cameras", len(cams))
}

func newLogger(cfg config.Config) *slog.Logger {
	options := &slog.HandlerOptions{
		Level: config.ParseLogLevel(cfg.LogLevel),
	}

	if cfg.Environment == "development" {
		options.AddSource = true
	}

	handler := slog.NewJSONHandler(os.Stdout, options)

	return slog.New(handler)
}