package timesync

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/onvif"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

// Ключи настроек синхронизации времени.
const (
	KeyTimeSyncEnabled = "time_sync_enabled"
	KeyTimeSyncTZ      = "time_sync_tz" // auto | posix | naive | utc
)

const (
	// interval — периодичность синхронизации: один раз в час.
	interval = 1 * time.Hour
	// checkEvery — периодичность проверки включённости настройки.
	checkEvery = 1 * time.Minute
	// applyTolerance — допустимое расхождение после установки, секунд.
	applyTolerance = 3
)

// Result — результат синхронизации одной камеры с диагностикой.
type Result struct {
	CameraID       uuid.UUID `json:"camera_id"`
	Name           string    `json:"name"`
	OK             bool      `json:"ok"`
	Error          string    `json:"error,omitempty"`
	CameraTZ       string    `json:"camera_tz"`
	DriftBeforeSec int64     `json:"drift_before_sec"`
	DriftAfterSec  int64     `json:"drift_after_sec"`
	TZSent         string    `json:"tz_sent"`
}

// Manager синхронизирует время ONVIF-камер с часами ПК.
type Manager struct {
	cameraRepo *postgres.CameraRepository
	settings   *postgres.SettingsRepository
	logger     *slog.Logger

	mu       sync.Mutex
	lastSync time.Time
}

// NewManager создает менеджер синхронизации времени.
func NewManager(
	cameraRepo *postgres.CameraRepository,
	settings *postgres.SettingsRepository,
	logger *slog.Logger,
) *Manager {
	return &Manager{cameraRepo: cameraRepo, settings: settings, logger: logger}
}

var (
	defaultMu  sync.Mutex
	defaultMgr *Manager
)

// SetDefault регистрирует менеджер для доступа из HTTP-обработчиков.
func SetDefault(m *Manager) {
	defaultMu.Lock()
	defaultMgr = m
	defaultMu.Unlock()
}

// Default возвращает зарегистрированный менеджер (или nil).
func Default() *Manager {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	return defaultMgr
}

// Start запускает цикл: проверка настройки каждую минуту,
// синхронизация не чаще одного раза в час.
func (m *Manager) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(checkEvery)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.tick(ctx)
			}
		}
	}()
}

func (m *Manager) tick(ctx context.Context) {
	all, err := m.settings.All(ctx)
	if err != nil {
		m.logger.Error("timesync: failed to read settings", "error", err)
		return
	}
	if all[KeyTimeSyncEnabled] != "true" {
		return
	}

	m.mu.Lock()
	last := m.lastSync
	m.mu.Unlock()

	if time.Since(last) < interval {
		return
	}

	m.SyncNow(ctx)
}

// SyncNow немедленно синхронизирует время всех активных ONVIF-камер
// с проверкой применения и возвратом диагностики.
func (m *Manager) SyncNow(ctx context.Context) []Result {
	m.mu.Lock()
	m.lastSync = time.Now()
	m.mu.Unlock()

	all, err := m.settings.All(ctx)
	if err != nil {
		m.logger.Error("timesync: failed to read settings", "error", err)
		return nil
	}
	tzMode := all[KeyTimeSyncTZ]
	if tzMode == "" {
		tzMode = "auto"
	}

	cams, err := m.cameraRepo.List(ctx)
	if err != nil {
		m.logger.Error("timesync: failed to list cameras", "error", err)
		return nil
	}

	results := make([]Result, 0, len(cams))

	for _, cam := range cams {
		if cam.Status != domain.CameraStatusEnabled ||
			cam.SourceType != domain.SourceONVIF ||
			cam.ONVIF == nil {
			continue
		}
		results = append(results, m.syncCamera(ctx, cam, tzMode))
	}

	return results
}

func (m *Manager) syncCamera(ctx context.Context, cam domain.Camera, tzMode string) Result {
	port := cam.ONVIF.Port
	if port == 0 {
		port = 80
	}
	creds := onvif.Credentials{Username: cam.ONVIF.Username, Password: cam.ONVIF.Password}

	res := Result{CameraID: cam.ID, Name: cam.Name}
	now := time.Now()
	_, offset := now.Zone()
	hours := offset / 3600

	// 1. Читаем текущее время и пояс камеры.
	before, err := onvif.GetSystemDateAndTime(ctx, cam.ONVIF.Host, port, creds)
	if err != nil {
		res.Error = "get: " + err.Error()
		m.logger.Warn("timesync: get camera time failed", "camera", cam.Name, "error", err)
		return res
	}
	res.CameraTZ = before.TZ
	res.DriftBeforeSec = int64(math.Round(now.UTC().Sub(before.UTC).Seconds()))

	// 2. Выбираем пояс для отправки.
	tzToSend := before.TZ // auto: повторяем пояс камеры
	switch tzMode {
	case "posix":
		tzToSend = fmt.Sprintf("UTC%+d", -hours)
	case "naive":
		tzToSend = fmt.Sprintf("UTC%+d", hours)
	case "utc":
		tzToSend = "UTC0"
	}
	res.TZSent = tzToSend

	// 3. Устанавливаем время.
	if err := onvif.SetSystemDateAndTimeWithTZ(ctx, cam.ONVIF.Host, port, creds, now, tzToSend); err != nil {
		res.Error = "set: " + err.Error()
		m.logger.Warn("timesync: set camera time failed", "camera", cam.Name, "error", err)
		return res
	}

	// 4. Проверяем применение; при отказе с поясом повторяем без пояса.
	after, err := onvif.GetSystemDateAndTime(ctx, cam.ONVIF.Host, port, creds)
	if err == nil && math.Abs(now.UTC().Sub(after.UTC).Seconds()) > applyTolerance && tzToSend != "" {
		if err2 := onvif.SetSystemDateAndTimeWithTZ(ctx, cam.ONVIF.Host, port, creds, now, ""); err2 == nil {
			after, err = onvif.GetSystemDateAndTime(ctx, cam.ONVIF.Host, port, creds)
			res.TZSent = "(без TimeZone)"
		}
	}
	if err != nil {
		res.Error = "verify get: " + err.Error()
		return res
	}

	res.DriftAfterSec = int64(math.Round(now.UTC().Sub(after.UTC).Seconds()))
	if math.Abs(float64(res.DriftAfterSec)) > applyTolerance {
		res.Error = fmt.Sprintf("камера не применила время: дрейф после = %d с", res.DriftAfterSec)
		m.logger.Warn("timesync: camera did not apply time",
			"camera", cam.Name,
			"drift_after_sec", res.DriftAfterSec,
		)
		return res
	}

	res.OK = true
	m.logger.Info("timesync: camera time synchronized",
		"camera", cam.Name,
		"camera_tz", res.CameraTZ,
		"tz_sent", res.TZSent,
		"drift_before_sec", res.DriftBeforeSec,
		"drift_after_sec", res.DriftAfterSec,
		"pc_time", now.Format(time.RFC3339),
	)
	return res
}