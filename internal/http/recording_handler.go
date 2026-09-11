package httpapi

import (
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

// handleListRecordings возвращает сегменты архива с фильтрами.
func (h *Handler) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var cameraID *uuid.UUID
	if v := q.Get("camera_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid camera_id"})
			return
		}
		cameraID = &id
	}

	var from, to *time.Time
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid from: use RFC3339"})
			return
		}
		from = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid to: use RFC3339"})
			return
		}
		to = &t
	}

	recordings, err := h.recordingRepo.List(r.Context(), cameraID, from, to)
	if err != nil {
		h.logger.Error("failed to list recordings", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if recordings == nil {
		recordings = []domain.Recording{}
	}

	writeJSON(w, http.StatusOK, recordings)
}

// handleGetRecordingFile отдает файл сегмента для воспроизведения или скачивания.
// Перед отдачей проверяет существование файла на диске.
func (h *Handler) handleGetRecordingFile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid recording id"})
		return
	}

	rec, err := h.recordingRepo.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to get recording", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if rec == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "recording not found"})
		return
	}

	// Проверка существования файла на диске.
	if _, err := os.Stat(rec.StoragePath); err != nil {
		if os.IsNotExist(err) {
			// Метаданные устарели: файл удален с диска.
			// Удаляем строку из базы, чтобы список архива стал консистентным.
			if delErr := h.recordingRepo.Delete(r.Context(), rec.ID); delErr != nil {
				h.logger.Error("failed to delete stale recording row",
					"recording_id", rec.ID,
					"error", delErr,
				)
			}

			writeJSON(w, http.StatusNotFound, map[string]string{"error": "recording file not found on disk"})
			return
		}

		h.logger.Error("failed to stat recording file",
			"path", rec.StoragePath,
			"error", err,
		)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	http.ServeFile(w, r, rec.StoragePath)
}