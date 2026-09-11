package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/onvif"
)

// CreateCameraRequest описывает тело запроса на создание камеры.
type CreateCameraRequest struct {
	Name            string                  `json:"name"`
	RTSPUri         string                  `json:"rtsp_uri"`
	Location        string                  `json:"location"`
	FireZoneID      *uuid.UUID              `json:"fire_zone_id"`
	Config          map[string]any          `json:"config"`
	SourceType      domain.CameraSourceType `json:"source_type"`
	ONVIF           *domain.ONVIFParams     `json:"onvif"`
	RecordingMode   domain.RecordingMode    `json:"recording_mode"`
	MotionDetection *bool                   `json:"motion_detection"`
}

// UpdateCameraRequest описывает тело запроса на обновление камеры.
type UpdateCameraRequest struct {
	Name            *string                  `json:"name"`
	RTSPUri         *string                  `json:"rtsp_uri"`
	Location        *string                  `json:"location"`
	FireZoneID      *uuid.UUID               `json:"fire_zone_id"`
	Status          *domain.CameraStatus     `json:"status"`
	Config          map[string]any           `json:"config"`
	SourceType      *domain.CameraSourceType `json:"source_type"`
	ONVIF           *domain.ONVIFParams      `json:"onvif"`
	RecordingMode   *domain.RecordingMode    `json:"recording_mode"`
	MotionDetection *bool                    `json:"motion_detection"`
}

// handleCreateCamera создает новую камеру.
func (h *Handler) handleCreateCamera(w http.ResponseWriter, r *http.Request) {
	var req CreateCameraRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	sourceType := req.SourceType
	if sourceType == "" {
		sourceType = domain.SourceRTSP
	}

	cam := &domain.Camera{
		Name:          req.Name,
		Location:      req.Location,
		FireZoneID:    req.FireZoneID,
		Status:        domain.CameraStatusEnabled,
		Config:        req.Config,
		SourceType:    sourceType,
		RecordingMode: req.RecordingMode,
	}

	if req.MotionDetection != nil {
		cam.MotionDetection = *req.MotionDetection
	}

	switch sourceType {
	case domain.SourceONVIF:
		if req.ONVIF == nil || req.ONVIF.Host == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "onvif host is required"})
			return
		}
		port := req.ONVIF.Port
		if port == 0 {
			port = 80
		}
		req.ONVIF.Port = port

		uri, err := onvif.GetStreamUri(r.Context(), req.ONVIF.Host, port,
			onvif.Credentials{Username: req.ONVIF.Username, Password: req.ONVIF.Password},
			req.ONVIF.Profile)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "onvif: " + err.Error()})
			return
		}

		cam.RTSPUri = domain.NormalizeRTSPUri(uri)
		cam.ONVIF = req.ONVIF

	case domain.SourceRTSP:
		cam.RTSPUri = domain.NormalizeRTSPUri(req.RTSPUri)

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid source type"})
		return
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

	pathName := "cam_" + cam.ID.String()
	if err := h.media.AddPath(r.Context(), pathName, cam.RTSPUri); err != nil {
		h.logger.Error("failed to register stream path in mediamtx",
			"camera_id", cam.ID.String(),
			"error", err,
		)
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
		cameras = []domain.Camera{}
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

	host := "localhost"
	pathID := fmt.Sprintf("cam_%s", cam.ID.String())

	streamInfo := map[string]string{
		"camera_id":  cam.ID.String(),
		"rtsp_url":   fmt.Sprintf("rtsp://%s:8554/%s", host, pathID),
		"hls_url":    fmt.Sprintf("http://%s:8888/%s/index.m3u8", host, pathID),
		"webrtc_url": fmt.Sprintf("http://%s:8889/%s", host, pathID),
	}

	writeJSON(w, http.StatusOK, streamInfo)
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

	if req.Name != nil {
		cam.Name = *req.Name
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
	if req.SourceType != nil {
		cam.SourceType = *req.SourceType
	}
	if req.ONVIF != nil {
		cam.ONVIF = req.ONVIF
	}
	if req.RecordingMode != nil {
		cam.RecordingMode = *req.RecordingMode
	}
	if req.MotionDetection != nil {
		cam.MotionDetection = *req.MotionDetection
	}

	switch cam.SourceType {
	case domain.SourceONVIF:
		if cam.ONVIF == nil || cam.ONVIF.Host == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "onvif host is required"})
			return
		}
		port := cam.ONVIF.Port
		if port == 0 {
			port = 80
			cam.ONVIF.Port = port
		}

		uri, err := onvif.GetStreamUri(r.Context(), cam.ONVIF.Host, port,
			onvif.Credentials{Username: cam.ONVIF.Username, Password: cam.ONVIF.Password},
			cam.ONVIF.Profile)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "onvif: " + err.Error()})
			return
		}
		cam.RTSPUri = domain.NormalizeRTSPUri(uri)

	case domain.SourceRTSP:
		if req.RTSPUri != nil {
			cam.RTSPUri = domain.NormalizeRTSPUri(*req.RTSPUri)
		}

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid source type"})
		return
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

	pathName := "cam_" + cam.ID.String()
	if cam.Status == domain.CameraStatusEnabled {
		if err := h.media.AddPath(r.Context(), pathName, cam.RTSPUri); err != nil {
			h.logger.Error("failed to update stream path in mediamtx",
				"camera_id", cam.ID.String(),
				"error", err,
			)
		}
	} else {
		if err := h.media.RemovePath(r.Context(), pathName); err != nil {
			h.logger.Error("failed to remove stream path from mediamtx",
				"camera_id", cam.ID.String(),
				"error", err,
			)
		}
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

	if err := h.media.RemovePath(r.Context(), "cam_"+id.String()); err != nil {
		h.logger.Error("failed to remove stream path from mediamtx",
			"camera_id", id.String(),
			"error", err,
		)
	}

	w.WriteHeader(http.StatusNoContent)
}
