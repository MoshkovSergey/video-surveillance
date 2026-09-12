package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gitverse.ru/cataclysm78/video-surveillance/internal/auth"
	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/mediamtx"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

// Handler содержит зависимости HTTP-сервера.
type Handler struct {
	pool          *pgxpool.Pool
	cameraRepo    *postgres.CameraRepository
	recordingRepo *postgres.RecordingRepository
	eventRepo     *postgres.EventRepository
	userRepo      *postgres.UserRepository
	media         *mediamtx.Client
	tokens        *auth.TokenService
	storageRoot   string
	logger        *slog.Logger
}

// NewHandler создает HTTP-обработчик и регистрирует маршруты.
func NewHandler(
	pool *pgxpool.Pool,
	cameraRepo *postgres.CameraRepository,
	recordingRepo *postgres.RecordingRepository,
	eventRepo *postgres.EventRepository,
	userRepo *postgres.UserRepository,
	media *mediamtx.Client,
	tokens *auth.TokenService,
	storageRoot string,
	logger *slog.Logger,
) http.Handler {
	h := &Handler{
		pool:          pool,
		cameraRepo:    cameraRepo,
		recordingRepo: recordingRepo,
		eventRepo:     eventRepo,
		userRepo:      userRepo,
		media:         media,
		tokens:        tokens,
		storageRoot:   storageRoot,
		logger:        logger,
	}

	mux := http.NewServeMux()

	// Health checks (public)
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.HandleFunc("GET /healthz/db", h.handleHealthzDB)

	// Auth (public)
	mux.HandleFunc("POST /api/v1/auth/login", h.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.handleRefresh)

	// Auth (protected)
	mux.HandleFunc("GET /api/v1/auth/me", h.requireAuth(h.handleMe))

	// Discovery (admin only)
	mux.HandleFunc("POST /api/v1/discovery/scan", h.requireRoles(h.handleDiscoveryScan, domain.RoleAdmin))

	// ONVIF (admin/operator)
	mux.HandleFunc("POST /api/v1/onvif/probe", h.requireRoles(h.handleOnvifProbe, domain.RoleAdmin, domain.RoleOperator))

	// Cameras API
	mux.HandleFunc("POST /api/v1/cameras", h.requireRoles(h.handleCreateCamera, domain.RoleAdmin, domain.RoleOperator))
	mux.HandleFunc("GET /api/v1/cameras", h.requireAuth(h.handleListCameras))
	mux.HandleFunc("GET /api/v1/cameras/{id}", h.requireAuth(h.handleGetCamera))
	mux.HandleFunc("GET /api/v1/cameras/{id}/stream", h.requireAuth(h.handleGetCameraStream))
	mux.HandleFunc("PATCH /api/v1/cameras/{id}", h.requireRoles(h.handleUpdateCamera, domain.RoleAdmin, domain.RoleOperator))
	mux.HandleFunc("DELETE /api/v1/cameras/{id}", h.requireRoles(h.handleDeleteCamera, domain.RoleAdmin))

	// Живой звук: транскодинг G.711 -> AAC для браузерного просмотра
	mux.HandleFunc("POST /api/v1/cameras/{id}/audio/start", h.requireAuth(h.handleAudioStart))
	mux.HandleFunc("POST /api/v1/cameras/{id}/audio/stop", h.requireAuth(h.handleAudioStop))
	mux.HandleFunc("GET /api/v1/cameras/{id}/audio/status", h.requireAuth(h.handleAudioStatus))

	// Recordings API
	mux.HandleFunc("GET /api/v1/recordings", h.requireAuth(h.handleListRecordings))
	mux.HandleFunc("GET /api/v1/recordings/{id}/file", h.requireAuth(h.handleGetRecordingFile))

	// Events API
	mux.HandleFunc("GET /api/v1/events", h.requireAuth(h.handleListEvents))

	// Users API (admin only)
	mux.HandleFunc("GET /api/v1/users", h.requireRoles(h.handleListUsers, domain.RoleAdmin))
	mux.HandleFunc("POST /api/v1/users", h.requireRoles(h.handleCreateUser, domain.RoleAdmin))
	mux.HandleFunc("PATCH /api/v1/users/{id}", h.requireRoles(h.handleUpdateUser, domain.RoleAdmin))
	mux.HandleFunc("DELETE /api/v1/users/{id}", h.requireRoles(h.handleDeleteUser, domain.RoleAdmin))

	// Settings API (admin only)
	mux.HandleFunc("GET /api/v1/settings", h.requireRoles(h.handleGetSettings, domain.RoleAdmin))
	mux.HandleFunc("PUT /api/v1/settings", h.requireRoles(h.handleUpdateSettings, domain.RoleAdmin))
	mux.HandleFunc("POST /api/v1/settings/telegram/test", h.requireRoles(h.handleTelegramTest, domain.RoleAdmin))

	// Планы объекта (e-map): просмотр — всем, управление — admin
	mux.HandleFunc("GET /api/v1/plans", h.requireAuth(h.handleListPlans))
	mux.HandleFunc("POST /api/v1/plans", h.requireRoles(h.handleCreatePlan, domain.RoleAdmin))
	mux.HandleFunc("GET /api/v1/plans/{id}", h.requireAuth(h.handleGetPlan))
	mux.HandleFunc("GET /api/v1/plans/{id}/image", h.requireAuth(h.handleGetPlanImage))
	mux.HandleFunc("PATCH /api/v1/plans/{id}", h.requireRoles(h.handleRenamePlan, domain.RoleAdmin))
	mux.HandleFunc("DELETE /api/v1/plans/{id}", h.requireRoles(h.handleDeletePlan, domain.RoleAdmin))
	mux.HandleFunc("PUT /api/v1/plans/{id}/objects", h.requireRoles(h.handlePutPlanObjects, domain.RoleAdmin))
	mux.HandleFunc("GET /api/v1/plans/{id}/status", h.requireAuth(h.handlePlanStatus))
	mux.HandleFunc("POST /api/v1/settings/time-sync/run", h.requireRoles(h.handleTimeSyncRun, domain.RoleAdmin))

	return h.recover(h.logRequests(h.authMiddleware(mux)))
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
					"message": "внутренняя ошибка сервера",
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
		return
	}
}

// decodeJSON декодирует тело запроса в структуру.
func decodeJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}
