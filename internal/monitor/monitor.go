package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/mediamtx"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

// Monitor периодически опрашивает состояние путей MediaMTX
// и фиксирует события потери и восстановления связи с камерами.
type Monitor struct {
	media      *mediamtx.Client
	cameraRepo *postgres.CameraRepository
	eventRepo  *postgres.EventRepository
	interval   time.Duration
	logger     *slog.Logger

	mu    sync.Mutex
	state map[uuid.UUID]bool
}

// NewMonitor создает монитор состояния камер.
func NewMonitor(
	media *mediamtx.Client,
	cameraRepo *postgres.CameraRepository,
	eventRepo *postgres.EventRepository,
	interval time.Duration,
	logger *slog.Logger,
) *Monitor {
	return &Monitor{
		media:      media,
		cameraRepo: cameraRepo,
		eventRepo:  eventRepo,
		interval:   interval,
		logger:     logger,
		state:      make(map[uuid.UUID]bool),
	}
}

// Start запускает цикл мониторинга в отдельной горутине.
func (m *Monitor) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		m.check(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.check(ctx)
			}
		}
	}()
}

func (m *Monitor) check(ctx context.Context) {
	paths, err := m.media.ListPaths(ctx)
	if err != nil {
		m.logger.Error("failed to list mediamtx paths", "error", err)
		return
	}

	ready := make(map[string]bool, len(paths))
	for _, p := range paths {
		ready[p.Name] = p.Ready
	}

	cams, err := m.cameraRepo.List(ctx)
	if err != nil {
		m.logger.Error("failed to list cameras for monitoring", "error", err)
		return
	}

	for _, cam := range cams {
		// Камеры, отключенные администратором, не мониторим.
		if cam.Status == domain.CameraStatusDisabled {
			continue
		}

		isReady := ready["cam_"+cam.ID.String()]

		m.mu.Lock()
		prev, seen := m.state[cam.ID]
		m.state[cam.ID] = isReady
		m.mu.Unlock()

		// Первое наблюдение после старта: если камера уже недоступна,
		// фиксируем событие, чтобы обрыв во время перезапуска не потерялся.
		if !seen {
			if !isReady {
				m.recordOffline(ctx, cam, true)
			}
			continue
		}

		if prev == isReady {
			continue
		}

		if isReady {
			m.recordOnline(ctx, cam)
		} else {
			m.recordOffline(ctx, cam, false)
		}
	}
}

// recordOnline фиксирует восстановление связи с камерой.
func (m *Monitor) recordOnline(ctx context.Context, cam domain.Camera) {
	ev := &domain.Event{
		CameraID:   &cam.ID,
		Type:       domain.EventCameraOnline,
		Severity:   domain.SeverityInfo,
		OccurredAt: time.Now(),
		Payload:    map[string]any{"path": "cam_" + cam.ID.String()},
	}
	if err := m.eventRepo.Create(ctx, ev); err != nil {
		m.logger.Error("failed to create camera_online event", "camera_id", cam.ID, "error", err)
	}

	if cam.Status == domain.CameraStatusError {
		if err := m.cameraRepo.SetStatus(ctx, cam.ID, domain.CameraStatusEnabled); err != nil {
			m.logger.Error("failed to restore camera status", "camera_id", cam.ID, "error", err)
		}
	}

	m.logger.Info("camera is online again", "camera_id", cam.ID)
}

// recordOffline фиксирует потерю связи с камерой.
// atStartup=true означает, что камера была недоступна уже в момент старта сервиса.
func (m *Monitor) recordOffline(ctx context.Context, cam domain.Camera, atStartup bool) {
	ev := &domain.Event{
		CameraID:   &cam.ID,
		Type:       domain.EventCameraOffline,
		Severity:   domain.SeverityCritical,
		OccurredAt: time.Now(),
		Payload: map[string]any{
			"path":       "cam_" + cam.ID.String(),
			"at_startup": atStartup,
		},
	}
	if err := m.eventRepo.Create(ctx, ev); err != nil {
		m.logger.Error("failed to create camera_offline event", "camera_id", cam.ID, "error", err)
	}

	if cam.Status == domain.CameraStatusEnabled {
		if err := m.cameraRepo.SetStatus(ctx, cam.ID, domain.CameraStatusError); err != nil {
			m.logger.Error("failed to set camera error status", "camera_id", cam.ID, "error", err)
		}
	}

	m.logger.Warn("camera lost", "camera_id", cam.ID, "at_startup", atStartup)
}
