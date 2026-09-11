package httpapi

import (
	"net/http"
	"strings"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/notify"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

type settingsDTO struct {
	TelegramEnabled     bool     `json:"telegram_enabled"`
	TelegramChatID      string   `json:"telegram_chat_id"`
	TelegramTokenSet    bool     `json:"telegram_bot_token_set"`
	TelegramTokenMasked string   `json:"telegram_bot_token_masked"`
	TelegramEvents      []string `json:"telegram_events"`
}

type updateSettingsRequest struct {
	TelegramEnabled *bool    `json:"telegram_enabled"`
	TelegramChatID  *string  `json:"telegram_chat_id"`
	TelegramToken   *string  `json:"telegram_bot_token"`
	TelegramEvents  []string `json:"telegram_events"`
}

type telegramTestRequest struct {
	TelegramToken  *string `json:"telegram_bot_token"`
	TelegramChatID *string `json:"telegram_chat_id"`
}

var allEventTypes = []string{
	string(domain.EventMotion),
	string(domain.EventCameraOnline),
	string(domain.EventCameraOffline),
	string(domain.EventFireAlarm),
	string(domain.EventSmokeDetection),
	string(domain.EventManualAlarm),
	string(domain.EventRecordingError),
}

func validEventType(t string) bool {
	for _, v := range allEventTypes {
		if v == t {
			return true
		}
	}
	return false
}

func maskToken(t string) string {
	if len(t) <= 10 {
		return "••••••"
	}
	return t[:6] + "…" + t[len(t)-4:]
}

func (h *Handler) settingsRepo() *postgres.SettingsRepository {
	return postgres.NewSettingsRepository(h.pool)
}

// handleGetSettings возвращает настройки уведомлений (только admin).
func (h *Handler) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	all, err := h.settingsRepo().All(r.Context())
	if err != nil {
		h.logger.Error("failed to read settings", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	dto := settingsDTO{
		TelegramEnabled: all[notify.KeyTelegramEnabled] == "true",
		TelegramChatID:  all[notify.KeyTelegramChatID],
	}
	if tok := all[notify.KeyTelegramToken]; tok != "" {
		dto.TelegramTokenSet = true
		dto.TelegramTokenMasked = maskToken(tok)
	}

	if evs := all[notify.KeyTelegramEvents]; evs == "" {
		dto.TelegramEvents = allEventTypes
	} else {
		dto.TelegramEvents = strings.Split(evs, ",")
	}

	writeJSON(w, http.StatusOK, dto)
}

// handleUpdateSettings обновляет настройки уведомлений (только admin).
func (h *Handler) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректное тело запроса"})
		return
	}

	repo := h.settingsRepo()
	ctx := r.Context()

	if req.TelegramEnabled != nil {
		v := "false"
		if *req.TelegramEnabled {
			v = "true"
		}
		if err := repo.Set(ctx, notify.KeyTelegramEnabled, v); err != nil {
			h.logger.Error("failed to save setting", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
			return
		}
	}

	if req.TelegramChatID != nil {
		if err := repo.Set(ctx, notify.KeyTelegramChatID, strings.TrimSpace(*req.TelegramChatID)); err != nil {
			h.logger.Error("failed to save setting", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
			return
		}
	}

	// Пустой токен означает «не менять».
	if req.TelegramToken != nil && strings.TrimSpace(*req.TelegramToken) != "" {
		if err := repo.Set(ctx, notify.KeyTelegramToken, strings.TrimSpace(*req.TelegramToken)); err != nil {
			h.logger.Error("failed to save setting", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
			return
		}
	}

	if req.TelegramEvents != nil {
		clean := make([]string, 0, len(req.TelegramEvents))
		for _, t := range req.TelegramEvents {
			if validEventType(t) {
				clean = append(clean, t)
			}
		}
		if err := repo.Set(ctx, notify.KeyTelegramEvents, strings.Join(clean, ",")); err != nil {
			h.logger.Error("failed to save setting", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
			return
		}
	}

	h.handleGetSettings(w, r)
}

// handleTelegramTest отправляет тестовое сообщение (только admin).
func (h *Handler) handleTelegramTest(w http.ResponseWriter, r *http.Request) {
	var req telegramTestRequest
	_ = decodeJSON(r, &req)

	all, err := h.settingsRepo().All(r.Context())
	if err != nil {
		h.logger.Error("failed to read settings", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	token := all[notify.KeyTelegramToken]
	if req.TelegramToken != nil && strings.TrimSpace(*req.TelegramToken) != "" {
		token = strings.TrimSpace(*req.TelegramToken)
	}
	chat := all[notify.KeyTelegramChatID]
	if req.TelegramChatID != nil && strings.TrimSpace(*req.TelegramChatID) != "" {
		chat = strings.TrimSpace(*req.TelegramChatID)
	}

	if token == "" || chat == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "укажите токен бота и chat id"})
		return
	}

	if err := notify.SendTelegram(r.Context(), token, chat,
		"✅ Тестовое сообщение системы видеонаблюдения"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}