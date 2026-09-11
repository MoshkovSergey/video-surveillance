package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

// Ключи настроек Telegram-уведомлений.
const (
	KeyTelegramEnabled = "telegram_enabled"
	KeyTelegramToken   = "telegram_bot_token"
	KeyTelegramChatID  = "telegram_chat_id"
	KeyTelegramEvents  = "telegram_events" // список типов через запятую
)

const (
	sendTimeout = 10 * time.Second
	batchLimit  = 50
)

var eventTitles = map[domain.EventType]struct{ emoji, title string }{
	domain.EventMotion:         {emoji: "🚶", title: "Движение"},
	domain.EventCameraOnline:   {emoji: "✅", title: "Камера в сети"},
	domain.EventCameraOffline:  {emoji: "📛", title: "Потеря связи с камерой"},
	domain.EventFireAlarm:      {emoji: "🔥", title: "Пожарная тревога"},
	domain.EventSmokeDetection: {emoji: "💨", title: "Обнаружение дыма"},
	domain.EventManualAlarm:    {emoji: "🚨", title: "Ручная тревога"},
	domain.EventRecordingError: {emoji: "⛔", title: "Ошибка записи"},
}

// Notifier опрашивает журнал событий и отправляет уведомления в Telegram.
type Notifier struct {
	eventRepo   *postgres.EventRepository
	cameraRepo  *postgres.CameraRepository
	settings    *postgres.SettingsRepository
	logger      *slog.Logger
	interval    time.Duration
	storageRoot string

	lastTs time.Time
	seen   map[uuid.UUID]bool
}

// NewNotifier создает отправитель уведомлений.
func NewNotifier(
	eventRepo *postgres.EventRepository,
	cameraRepo *postgres.CameraRepository,
	settings *postgres.SettingsRepository,
	logger *slog.Logger,
	interval time.Duration,
	storageRoot string,
) *Notifier {
	return &Notifier{
		eventRepo:   eventRepo,
		cameraRepo:  cameraRepo,
		settings:    settings,
		logger:      logger,
		interval:    interval,
		storageRoot: storageRoot,
		lastTs:      time.Now(), // ретроспектива не рассылается
		seen:        make(map[uuid.UUID]bool),
	}
}

// Start запускает цикл опроса журнала событий.
func (n *Notifier) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(n.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n.process(ctx)
			}
		}
	}()
}

type notifyConfig struct {
	enabled bool
	token   string
	chat    string
	events  map[string]bool
}

func (n *Notifier) config(ctx context.Context) notifyConfig {
	all, err := n.settings.All(ctx)
	if err != nil {
		n.logger.Error("notify: failed to read settings", "error", err)
		return notifyConfig{}
	}

	cfg := notifyConfig{
		enabled: all[KeyTelegramEnabled] == "true",
		token:   all[KeyTelegramToken],
		chat:    all[KeyTelegramChatID],
		events:  make(map[string]bool),
	}

	raw := all[KeyTelegramEvents]
	if strings.TrimSpace(raw) == "" {
		for t := range eventTitles {
			cfg.events[string(t)] = true
		}
	} else {
		for _, t := range strings.Split(raw, ",") {
			cfg.events[strings.TrimSpace(t)] = true
		}
	}
	return cfg
}

func (n *Notifier) process(ctx context.Context) {
	cfg := n.config(ctx)

	events, err := n.eventRepo.ListSince(ctx, n.lastTs, batchLimit)
	if err != nil {
		n.logger.Error("notify: failed to list events", "error", err)
		return
	}
	if len(events) == 0 {
		n.lastTs = time.Now()
		return
	}

	names := make(map[uuid.UUID]string)
	if cams, err := n.cameraRepo.List(ctx); err == nil {
		for _, cam := range cams {
			names[cam.ID] = cam.Name
		}
	}

	maxTs := n.lastTs
	for _, ev := range events {
		if ev.OccurredAt.After(maxTs) {
			maxTs = ev.OccurredAt
		}
		if n.seen[ev.ID] {
			continue
		}
		n.seen[ev.ID] = true

		if !cfg.enabled || cfg.token == "" || cfg.chat == "" {
			continue
		}
		if !cfg.events[string(ev.Type)] {
			continue
		}

		text := n.message(ev, names)

		// События движения отправляем со снимком кадра, если он есть.
		if rel, ok := ev.Payload["snapshot"].(string); ok && rel != "" {
			photo := filepath.Join(n.storageRoot, filepath.FromSlash(rel))
			if _, err := os.Stat(photo); err == nil {
				if err := SendTelegramPhoto(ctx, cfg.token, cfg.chat, photo, text); err != nil {
					n.logger.Warn("notify: telegram photo send failed", "event_id", ev.ID, "error", err)
					continue
				}
				n.logger.Info("notify: telegram photo notification sent",
					"event_id", ev.ID,
					"type", string(ev.Type),
				)
				continue
			}
		}

		if err := SendTelegram(ctx, cfg.token, cfg.chat, text); err != nil {
			n.logger.Warn("notify: telegram send failed", "event_id", ev.ID, "error", err)
			continue
		}
		n.logger.Info("notify: telegram notification sent",
			"event_id", ev.ID,
			"type", string(ev.Type),
		)
	}

	n.lastTs = maxTs
	if len(n.seen) > 1000 {
		n.seen = make(map[uuid.UUID]bool)
	}
}

func (n *Notifier) message(ev domain.Event, names map[uuid.UUID]string) string {
	t, ok := eventTitles[ev.Type]
	if !ok {
		t = struct{ emoji, title string }{emoji: "ℹ️", title: string(ev.Type)}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s <b>%s</b>\n", t.emoji, t.title))

	if ev.CameraID != nil {
		name := names[*ev.CameraID]
		if name == "" {
			name = ev.CameraID.String()
		}
		sb.WriteString("Камера: " + escapeHTML(name) + "\n")
	}

	sb.WriteString("Время: " + ev.OccurredAt.Format("02.01.2006 15:04:05") + "\n")

	if d, ok := ev.Payload["duration_sec"].(float64); ok {
		sb.WriteString(fmt.Sprintf("Длительность: %d с\n", int(d)))
	}
	return sb.String()
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// SendTelegram отправляет текстовое сообщение через Bot API Telegram.
func SendTelegram(ctx context.Context, token, chat, text string) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	payload := map[string]string{
		"chat_id":                  chat,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": "true",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		var tg struct {
			Description string `json:"description"`
		}
		_ = json.NewDecoder(res.Body).Decode(&tg)
		return fmt.Errorf("telegram api: статус %d: %s", res.StatusCode, tg.Description)
	}
	return nil
}

// SendTelegramPhoto отправляет сообщение с фотографией через Bot API Telegram.
func SendTelegramPhoto(ctx context.Context, token, chat, photoPath, caption string) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout+10*time.Second)
	defer cancel()

	f, err := os.Open(photoPath)
	if err != nil {
		return fmt.Errorf("open photo: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if err := mw.WriteField("chat_id", chat); err != nil {
		return fmt.Errorf("telegram multipart: %w", err)
	}
	if err := mw.WriteField("caption", caption); err != nil {
		return fmt.Errorf("telegram multipart: %w", err)
	}
	if err := mw.WriteField("parse_mode", "HTML"); err != nil {
		return fmt.Errorf("telegram multipart: %w", err)
	}

	part, err := mw.CreateFormFile("photo", filepath.Base(photoPath))
	if err != nil {
		return fmt.Errorf("telegram multipart: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return fmt.Errorf("telegram multipart copy: %w", err)
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("telegram multipart close: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/sendPhoto", &buf)
	if err != nil {
		return fmt.Errorf("create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		var tg struct {
			Description string `json:"description"`
		}
		_ = json.NewDecoder(res.Body).Decode(&tg)
		return fmt.Errorf("telegram api: статус %d: %s", res.StatusCode, tg.Description)
	}
	return nil
}