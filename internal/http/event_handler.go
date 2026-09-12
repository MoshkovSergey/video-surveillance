package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

// handleListEvents возвращает события журнала с фильтрами и пагинацией.
func (h *Handler) handleListEvents(w http.ResponseWriter, r *http.Request) {
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

	var typ *domain.EventType
	if v := q.Get("type"); v != "" {
		t := domain.EventType(v)
		typ = &t
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

	events, err := h.eventRepo.List(r.Context(), cameraID, typ, from, to, limit, offset)
	if err != nil {
		h.logger.Error("failed to list events", "error", err)
		// Временно возвращаем точную причину для диагностики.
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, events)
}
