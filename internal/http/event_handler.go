package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

// allowedEventTypes — белый список допустимых значений фильтра type.
var allowedEventTypes = map[string]struct{}{
	string(domain.EventMotion):         {},
	string(domain.EventCameraOnline):   {},
	string(domain.EventCameraOffline):  {},
	string(domain.EventFireAlarm):      {},
	string(domain.EventSmokeDetection): {},
	string(domain.EventManualAlarm):    {},
	string(domain.EventRecordingError): {},
}

// handleListEvents возвращает журнал событий с фильтрами.
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

	eventType := q.Get("type")
	if eventType != "" {
		if _, ok := allowedEventTypes[eventType]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid type"})
			return
		}
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

	limit := 200
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		if n > 1000 {
			n = 1000
		}
		limit = n
	}

	events, err := h.eventRepo.List(r.Context(), cameraID, eventType, from, to, limit)
	if err != nil {
		h.logger.Error("failed to list events", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if events == nil {
		events = []domain.Event{}
	}

	writeJSON(w, http.StatusOK, events)
}