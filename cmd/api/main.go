package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"gitverse.ru/cataclysm78/video-surveillance/internal/auth"
	"gitverse.ru/cataclysm78/video-surveillance/internal/clipper"
	"gitverse.ru/cataclysm78/video-surveillance/internal/config"
	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	httpapi "gitverse.ru/cataclysm78/video-surveillance/internal/http"
	"gitverse.ru/cataclysm78/video-surveillance/internal/mediamtx"
	"gitverse.ru/cataclysm78/video-surveillance/internal/monitor"
	"gitverse.ru/cataclysm78/video-surveillance/internal/motion"
	"gitverse.ru/cataclysm78/video-surveillance/internal/notify"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
	"gitverse.ru/cataclysm78/video-surveillance/internal/recorder"
)

func main() {
	loadEnv()


	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("application stopped with error", "error", err)
		os.Exit(1)
	}
}

// loadEnv читает .env сначала рядом с исполняемым файлом, затем из CWD.
func loadEnv() {
	if exe, err := os.Executable(); err == nil {
		_ = godotenv.Load(filepath.Join(filepath.Dir(exe), ".env"))
	}
	_ = godotenv.Load()
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)

	if cfg.JWTSecret == "dev-secret-change-me" {
		logger.Warn("JWT_SECRET uses development default; change it before production use")
	}


	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		return err
	}
	defer pool.Close()

	logger.Info("database connection established")

	cameraRepo := postgres.NewCameraRepository(pool)
	recordingRepo := postgres.NewRecordingRepository(pool)
	eventRepo := postgres.NewEventRepository(pool)
	userRepo := postgres.NewUserRepository(pool)
	clipJobRepo := postgres.NewClipJobRepository(pool)
	settingsRepo := postgres.NewSettingsRepository(pool)

	media := mediamtx.NewClient(cfg.MediaMTXAPIURL)
	tokens := auth.NewTokenService(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)

	if err := seedAdmin(ctx, userRepo, cfg, logger); err != nil {
		logger.Error("failed to seed admin user", "error", err)
		return err
	}

	syncMediaPaths(ctx, cameraRepo, media, logger)

	// Отправитель уведомлений в Telegram по событиям журнала.
	// Создается ДО scanner, так как scanner использует его для отправки видео.
	notifier := notify.NewNotifier(
		eventRepo,
		cameraRepo,
		settingsRepo,
		logger,
		10*time.Second,
		cfg.StoragePath,
	)
	notifier.Start(ctx)

	// Сканер записей: синхронизация, ротация, кадрирование, отправка видео.
	scanner := recorder.NewScanner(
		recordingRepo,
		cameraRepo,
		clipJobRepo,
		notifier,
		filepath.Join(cfg.StoragePath, "recordings"),
		30*time.Second,
		logger,
	)
	scanner.Start(ctx)

	// Монитор доступности камер.
	mon := monitor.NewMonitor(media, cameraRepo, eventRepo, 10*time.Second, logger)
	mon.Start(ctx)

	// Менеджер детекции движения по событиям ONVIF.
	motionMgr := motion.NewManager(cameraRepo, eventRepo, clipJobRepo, clipper.New(cfg.StoragePath), logger)
	motionMgr.Start(ctx)

	// HTTP API и встроенный веб-интерфейс.
	absStorage, err := filepath.Abs(cfg.StoragePath)
	if err != nil {
		absStorage = cfg.StoragePath
	}
	api := httpapi.NewHandler(pool, cameraRepo, recordingRepo, eventRepo, userRepo, media, tokens, absStorage, logger)
	

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/healthz") {
			api.ServeHTTP(w, r)
			return
		}
		
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           final,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api server starting", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		logger.Error("api server stopped unexpectedly", "error", err)
		shutdown(server, cfg, logger)
		return err
	}

	shutdown(server, cfg, logger)
	logger.Info("server stopped")
	return nil
}

func shutdown(server *http.Server, cfg config.Config, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}
}

// seedAdmin создает первого администратора при пустой таблице users.
func seedAdmin(ctx context.Context, repo *postgres.UserRepository, cfg config.Config, logger *slog.Logger) error {
	count, err := repo.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}

	admin := &domain.User{
		Username:     cfg.AdminUsername,
		PasswordHash: hash,
		Role:         domain.RoleAdmin,
		IsActive:     true,
	}
	if err := repo.Create(ctx, admin); err != nil {
		return err
	}

	logger.Warn("created initial admin user; change its password in production",
		"username", admin.Username,
	)
	return nil
}

// syncMediaPaths регистрирует пути MediaMTX для всех активных камер из базы данных.
func syncMediaPaths(ctx context.Context, repo *postgres.CameraRepository, media *mediamtx.Client, logger *slog.Logger) {
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