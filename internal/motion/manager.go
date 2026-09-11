package motion

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/onvif"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

const (
	// reconcileInterval — период сверки списка камер.
	reconcileInterval = 30 * time.Second
	// episodeCooldown — пауза без движения, завершающая эпизод.
	episodeCooldown = 10 * time.Second
	// preRoll — предзапись: окно архива начинается за 5 с до триггера.
	preRoll = 5 * time.Second
	// postRoll — запас после окончания эпизода.
	postRoll = 30 * time.Second
	// resubscribeAfter — срок жизни PullPoint-подписки.
	resubscribeAfter = 4 * time.Minute
)

// Manager управляет воркерами детекции движения по событиям ONVIF.
type Manager struct {
	cameraRepo *postgres.CameraRepository
	eventRepo  *postgres.EventRepository
	recRepo    *postgres.RecordingRepository
	logger     *slog.Logger

	mu      sync.Mutex
	workers map[uuid.UUID]context.CancelFunc
}

// NewManager создает менеджер детекции движения.
func NewManager(
	cameraRepo *postgres.CameraRepository,
	eventRepo *postgres.EventRepository,
	recRepo *postgres.RecordingRepository,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		cameraRepo: cameraRepo,
		eventRepo:  eventRepo,
		recRepo:    recRepo,
		logger:     logger,
		workers:    make(map[uuid.UUID]context.CancelFunc),
	}
}

// Start запускает цикл сверки камер и воркеров детекции.
func (m *Manager) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(reconcileInterval)
		defer ticker.Stop()

		m.reconcile(ctx)
		for {
			select {
			case <-ctx.Done():
				m.stopAll()
				return
			case <-ticker.C:
				m.reconcile(ctx)
			}
		}
	}()
}

func (m *Manager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cancel := range m.workers {
		cancel()
		delete(m.workers, id)
	}
}

// reconcile синхронизирует набор воркеров с настройками камер.
func (m *Manager) reconcile(ctx context.Context) {
	cams, err := m.cameraRepo.List(ctx)
	if err != nil {
		m.logger.Error("motion: failed to list cameras", "error", err)
		return
	}

	want := make(map[uuid.UUID]domain.Camera)
	for _, cam := range cams {
		if cam.Status != domain.CameraStatusEnabled ||
			!cam.MotionDetection ||
			cam.SourceType != domain.SourceONVIF ||
			cam.ONVIF == nil {
			continue
		}
		want[cam.ID] = cam
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, cancel := range m.workers {
		if _, ok := want[id]; !ok {
			cancel()
			delete(m.workers, id)
		}
	}

	for id, cam := range want {
		if _, ok := m.workers[id]; ok {
			continue
		}
		wctx, cancel := context.WithCancel(context.Background())
		m.workers[id] = cancel
		go m.worker(wctx, cam)
	}
}

// worker слушает события одной камеры и формирует эпизоды движения.
func (m *Manager) worker(ctx context.Context, cam domain.Camera) {
	creds := onvif.Credentials{Username: cam.ONVIF.Username, Password: cam.ONVIF.Password}
	port := cam.ONVIF.Port
	if port == 0 {
		port = 80
	}

	var episodeStart, lastActive time.Time

	closeEpisode := func(end time.Time) {
		ev := &domain.Event{
			CameraID:   &cam.ID,
			Type:       domain.EventMotion,
			Severity:   domain.SeverityInfo,
			OccurredAt: end,
			Payload: map[string]any{
				"started_at":   episodeStart.Format(time.RFC3339),
				"ended_at":     end.Format(time.RFC3339),
				"duration_sec": int(end.Sub(episodeStart).Seconds()),
				"source":       "onvif",
			},
		}
		if err := m.eventRepo.Create(ctx, ev); err != nil {
			m.logger.Error("motion: failed to create event", "camera_id", cam.ID, "error", err)
		}

		// Окно архива: 5 с предзаписи до триггера и postRoll после окончания.
		// Помеченные сегменты хранятся постоянно и не удаляются буфером.
		kept, err := m.recRepo.MarkKept(ctx, cam.ID, episodeStart.Add(-preRoll), end.Add(postRoll))
		if err != nil {
			m.logger.Error("motion: failed to mark recordings kept", "camera_id", cam.ID, "error", err)
		}

		m.logger.Info("motion episode closed",
			"camera_id", cam.ID,
			"started_at", episodeStart.Format(time.RFC3339),
			"ended_at", end.Format(time.RFC3339),
			"recordings_kept", kept,
		)
		episodeStart = time.Time{}
	}

	for ctx.Err() == nil {
		subURL, err := onvif.Subscribe(ctx, cam.ONVIF.Host, port, creds)
		if err != nil {
			m.logger.Warn("motion: subscribe failed, retry later",
				"camera_id", cam.ID,
				"error", err,
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(reconcileInterval):
				continue
			}
		}

		subscribedAt := time.Now()

		for ctx.Err() == nil && time.Since(subscribedAt) < resubscribeAfter {
			msgs, err := onvif.PullMessages(ctx, subURL, creds)
			if err != nil {
				m.logger.Warn("motion: pull failed, resubscribe",
					"camera_id", cam.ID,
					"error", err,
				)
				break
			}

			now := time.Now()
			for _, msg := range msgs {
				if !msg.IsMotionTopic() {
					continue
				}
				if msg.IsActive() {
					if episodeStart.IsZero() {
						episodeStart = now
					}
					lastActive = now
				}
			}

			if !episodeStart.IsZero() && time.Since(lastActive) > episodeCooldown {
				closeEpisode(lastActive.Add(episodeCooldown))
			}
		}
	}

	if !episodeStart.IsZero() {
		closeEpisode(time.Now())
	}
}