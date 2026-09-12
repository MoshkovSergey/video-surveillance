package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/audio"
)

// audioMgr — процессовый менеджер звуковых сайкаров.
var audioMgr = audio.NewManager(nilLogger())

// handleAudioStart запускает транскодинг звука камеры для живого просмотра.
func (h *Handler) handleAudioStart(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор камеры"})
		return
	}

	cam, err := h.cameraRepo.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to get camera for audio", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}
	if cam == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "камера не найдена"})
		return
	}

	if err := audioMgr.Start(cam.ID, cam.RTSPUri); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "started",
		"running": true,
	})
}

// handleAudioStop останавливает транскодинг звука камеры.
func (h *Handler) handleAudioStop(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор камеры"})
		return
	}

	audioMgr.Stop(id)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "stopped",
		"running": false,
	})
}

// handleAudioStatus возвращает состояние звукового сайкара камеры.
func (h *Handler) handleAudioStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор камеры"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"running":   audioMgr.Running(id),
		"available": audioMgr.Available(),
	})
}
