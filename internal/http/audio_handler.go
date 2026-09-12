package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/audio"
)

// handleAudioStart запускает транскодер звука камеры.
// Источником служит поток MediaMTX (rtsp://127.0.0.1:8554/cam_<id>),
// а не оригинальный URL камеры — так избегаем ограничений RTSP-сеансов
// камеры и проблем доступности URL из контейнера.
func (h *Handler) handleAudioStart(w http.ResponseWriter, r *http.Request) {
	mgr := audio.Default()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "аудио-менеджер не запущен"})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор камеры"})
		return
	}

	// Убедимся, что камера существует.
	cams, err := h.cameraRepo.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}
	found := false
	for _, cam := range cams {
		if cam.ID == id {
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "камера не найдена"})
		return
	}

	// Источник: поток MediaMTX (живой, уже проверен снимками и плеером).
	source := fmt.Sprintf("rtsp://127.0.0.1:8554/cam_%s", id.String())

	if err := mgr.Start(id, source); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "started", "source": source})
}

// handleAudioStop останавливает транскодер звука камеры.
func (h *Handler) handleAudioStop(w http.ResponseWriter, r *http.Request) {
	mgr := audio.Default()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "аудио-менеджер не запущен"})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор камеры"})
		return
	}

	mgr.Stop(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// handleAudioStatus возвращает состояние сайкара и URL звукового HLS.
func (h *Handler) handleAudioStatus(w http.ResponseWriter, r *http.Request) {
	mgr := audio.Default()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "аудио-менеджер не запущен"})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор камеры"})
		return
	}

	st := mgr.Status(id)

	host := r.Host
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	audioHLS := fmt.Sprintf("http://%s:8888/cam_%s_audio/index.m3u8", host, id.String())

	writeJSON(w, http.StatusOK, map[string]any{
		"running":       st.Running,
		"mode":          st.Mode,
		"last_error":    st.LastError,
		"audio_hls_url": audioHLS,
	})
}