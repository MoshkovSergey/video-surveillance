package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

// handleListRecordings возвращает сегменты архива с фильтрами.
// kept=true — только клипы из storage/clips (страница «Архив»).
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

	var keptFilter *bool
	switch q.Get("kept") {
	case "true", "1":
		v := true
		keptFilter = &v
	case "false", "0":
		v := false
		keptFilter = &v
	}

	limit := 20
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	recordings, err := h.recordingRepo.List(r.Context(), cameraID, from, to, keptFilter, limit, offset)
	if err != nil {
		h.logger.Error("failed to list recordings", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// Отдаём только файлы, которые реально есть на диске.
	out := make([]domain.Recording, 0, len(recordings))
	for _, rec := range recordings {
		host := h.resolveRecordingPath(rec.StoragePath)
		fi, err := os.Stat(host)
		if err != nil {
			if os.IsNotExist(err) && rec.Kept {
				_ = h.recordingRepo.Delete(r.Context(), rec.ID)
			}
			continue
		}
		rec.StoragePath = filepath.ToSlash(host)
		rec.SizeBytes = fi.Size()
		out = append(out, rec)
	}

	writeJSON(w, http.StatusOK, out)
}

// handleGetRecordingFile отдает файл сегмента для воспроизведения или скачивания.
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

	host := h.resolveRecordingPath(rec.StoragePath)
	if _, err := os.Stat(host); err != nil {
		if os.IsNotExist(err) {
			if delErr := h.recordingRepo.Delete(r.Context(), rec.ID); delErr != nil {
				h.logger.Error("failed to delete stale recording row",
					"recording_id", rec.ID,
					"error", delErr,
				)
			}
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "recording file not found on disk"})
			return
		}
		h.logger.Error("failed to stat recording file", "path", host, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	http.ServeFile(w, r, host)
}

// resolveRecordingPath приводит путь из БД к абсолютному файлу.
func (h *Handler) resolveRecordingPath(storagePath string) string {
	p := filepath.Clean(filepath.FromSlash(storagePath))
	if filepath.IsAbs(p) {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if h.storageRoot == "" {
		return p
	}
	candidates := []string{
		filepath.Join(h.storageRoot, p),
		p,
	}
	slash := filepath.ToSlash(p)
	if i := strings.Index(slash, "clips/"); i >= 0 {
		candidates = append(candidates, filepath.Join(h.storageRoot, filepath.FromSlash(slash[i:])))
	}
	if i := strings.Index(slash, "recordings/"); i >= 0 {
		candidates = append(candidates, filepath.Join(h.storageRoot, filepath.FromSlash(slash[i:])))
	}
	base := filepath.Base(h.storageRoot)
	if strings.HasPrefix(slash, base+"/") {
		candidates = append(candidates, filepath.Join(h.storageRoot, filepath.FromSlash(strings.TrimPrefix(slash, base+"/"))))
	}
	for _, c := range candidates {
		if abs, err := filepath.Abs(c); err == nil {
			c = abs
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return filepath.Join(h.storageRoot, p)
}
