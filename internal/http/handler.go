package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gitverse.ru/cataclysm78/video-surveillance/internal/mediamtx"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

// Handler содержит зависимости HTTP-сервера.
type Handler struct {
	pool          *pgxpool.Pool
	cameraRepo    *postgres.CameraRepository
	recordingRepo *postgres.RecordingRepository
	media         *mediamtx.Client
	logger        *slog.Logger
}

// NewHandler создает HTTP-обработчик и регистрирует маршруты.
func NewHandler(
	pool *pgxpool.Pool,
	cameraRepo *postgres.CameraRepository,
	recordingRepo *postgres.RecordingRepository,
	media *mediamtx.Client,
	logger *slog.Logger,
) http.Handler {
	h := &Handler{
		pool:          pool,
		cameraRepo:    cameraRepo,
		recordingRepo: recordingRepo,
		media:         media,
		logger:        logger,
	}

	mux := http.NewServeMux()

	// Health checks
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.HandleFunc("GET /healthz/db", h.handleHealthzDB)

	// Cameras API
	mux.HandleFunc("POST /api/v1/cameras", h.handleCreateCamera)
	mux.HandleFunc("GET /api/v1/cameras", h.handleListCameras)
	mux.HandleFunc("GET /api/v1/cameras/{id}", h.handleGetCamera)
	mux.HandleFunc("GET /api/v1/cameras/{id}/stream", h.handleGetCameraStream)
	mux.HandleFunc("PATCH /api/v1/cameras/{id}", h.handleUpdateCamera)
	mux.HandleFunc("DELETE /api/v1/cameras/{id}", h.handleDeleteCamera)

	// Recordings API
	mux.HandleFunc("GET /api/v1/recordings", h.handleListRecordings)
	mux.HandleFunc("GET /api/v1/recordings/{id}/file", h.handleGetRecordingFile)

	return h.recover(h.logRequests(mux))
}

// handleHealthz возвращает базовый статус сервиса.
func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

// handleHealthzDB проверяет подключение к PostgreSQL.
func (h *Handler) handleHealthzDB(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	var one int

	if err := h.pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		h.logger.Error("database health check failed", "error", err)

		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status":   "error",
			"database": "unavailable",
		})

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"database": "ok",
	})
}

// logRequests записывает информацию о входящих запросах.
func (h *Handler) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		next.ServeHTTP(w, r)

		h.logger.Info(
			"http request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"duration", time.Since(start).String(),
		)
	})
}

// recover перехватывает panic и возвращает 500.
func (h *Handler) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				h.logger.Error("panic recovered", "panic", recovered, "path", r.URL.Path)

				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"status":  "error",
					"message": "internal server error",
				})
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// writeJSON сериализует ответ в JSON.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if body == nil {
		return
	}

	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Если ответ уже начал записываться, повторная запись статуса невозможна.
		return
	}
}