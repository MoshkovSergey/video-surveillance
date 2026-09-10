package httpapi

import (
	"encoding/json"
	"net/http"
	"fmt"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

// CreateCameraRequest описывает тело запроса на создание камеры.
type CreateCameraRequest struct {
	Name       string         `json:"name"`
	RTSPUri    string         `json:"rtsp_uri"`
	Location   string         `json:"location"`
	FireZoneID *uuid.UUID     `json:"fire_zone_id"`
	Config     map[string]any `json:"config"`
}

// UpdateCameraRequest описывает тело запроса на обновление камеры.
type UpdateCameraRequest struct {
	Name       *string         `json:"name"`
	RTSPUri    *string         `json:"rtsp_uri"`
	Location   *string         `json:"location"`
	FireZoneID *uuid.UUID      `json:"fire_zone_id"`
	Status     *domain.CameraStatus `json:"status"`
	Config     map[string]any  `json:"config"`
}

// handleCreateCamera создает новую камеру.
func (h *Handler) handleCreateCamera(w http.ResponseWriter, r *http.Request) {
	var req CreateCameraRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	cam := &domain.Camera{
		Name:       req.Name,
		RTSPUri:    req.RTSPUri,
		Location:   req.Location,
		FireZoneID: req.FireZoneID,
		Status:     domain.CameraStatusEnabled,
		Config:     req.Config,
	}

	if err := cam.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := h.cameraRepo.Create(r.Context(), cam); err != nil {
		h.logger.Error("failed to create camera", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusCreated, cam)
}

// handleListCameras возвращает список всех камер.
func (h *Handler) handleListCameras(w http.ResponseWriter, r *http.Request) {
	cameras, err := h.cameraRepo.List(r.Context())
	if err != nil {
		h.logger.Error("failed to list cameras", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if cameras == nil {
		cameras = []domain.Camera{} // Возвращаем пустой массив, а не null
	}
	writeJSON(w, http.StatusOK, cameras)
}

// handleGetCamera возвращает камеру по ID.
func (h *Handler) handleGetCamera(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid camera id"})
		return
	}

	cam, err := h.cameraRepo.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to get camera", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if cam == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "camera not found"})
		return
	}

	writeJSON(w, http.StatusOK, cam)
}

// handleUpdateCamera обновляет данные камеры.
func (h *Handler) handleUpdateCamera(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid camera id"})
		return
	}

	cam, err := h.cameraRepo.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to get camera for update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if cam == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "camera not found"})
		return
	}

	var req UpdateCameraRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Применяем частичные обновления
	if req.Name != nil {
		cam.Name = *req.Name
	}
	if req.RTSPUri != nil {
		cam.RTSPUri = *req.RTSPUri
	}
	if req.Location != nil {
		cam.Location = *req.Location
	}
	if req.FireZoneID != nil {
		cam.FireZoneID = req.FireZoneID
	}
	if req.Status != nil {
		cam.Status = *req.Status
	}
	if req.Config != nil {
		cam.Config = req.Config
	}

	if err := cam.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := h.cameraRepo.Update(r.Context(), cam); err != nil {
		h.logger.Error("failed to update camera", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, cam)
}

// handleDeleteCamera удаляет камеру.
func (h *Handler) handleDeleteCamera(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid camera id"})
		return
	}

	if err := h.cameraRepo.Delete(r.Context(), id); err != nil {
		h.logger.Error("failed to delete camera", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetCameraStream возвращает URL для подключения к видеопотоку через MediaMTX.
func (h *Handler) handleGetCameraStream(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid camera id"})
		return
	}

	cam, err := h.cameraRepo.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to get camera for stream", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if cam == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "camera not found"})
		return
	}

	// В продакшене хост и порты нужно брать из конфига.
	// Для локальной разработки используем localhost и стандартные порты MediaMTX.
	host := "localhost"
	
	// Используем префикс cam_ чтобы избежать коллизий путей в MediaMTX
	pathID := fmt.Sprintf("cam_%s", cam.ID.String())

	streamInfo := map[string]string{
		"camera_id":  cam.ID.String(),
		"rtsp_url":   fmt.Sprintf("rtsp://%s:8554/%s", host, pathID),
		"hls_url":    fmt.Sprintf("http://%s:8888/%s/index.m3u8", host, pathID),
		"webrtc_url": fmt.Sprintf("http://%s:8889/%s", host, pathID),
	}

	writeJSON(w, http.StatusOK, streamInfo)
}