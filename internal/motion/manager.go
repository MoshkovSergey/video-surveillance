package motion

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/clipper"
	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/onvif"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

const (
	// reconcileInterval — период сверки списка камер.
	reconcileInterval = 30 * time.Second
	// episodeCooldown — пауза без движения, завершающая эпизод.
	episodeCooldown = 10 * time.Second
	// preRoll — предзапись клипа: 5 секунд до триггера.
	preRoll = 5 * time.Second
	// postRoll — постзапись клипа: 5 секунд после окончания.
	postRoll = 5 * time.Second
	// resubscribeAfter — срок жизни PullPoint-подписки.
	resubscribeAfter = 4 * time.Minute
)

// mskZone — московское время (UTC+3, без сезонных переходов) для имён файлов.
var mskZone = time.FixedZone("MSK", 3*60*60)

// Manager управляет воркерами детекции движения по событиям ONVIF.
type Manager struct {
	cameraRepo *postgres.CameraRepository
	eventRepo  *postgres.EventRepository
	jobRepo    *postgres.ClipJobRepository
	clip       *clipper.Clipper
	logger     *slog.Logger

	mu      sync.Mutex
	workers map[uuid.UUID]context.CancelFunc
}

// NewManager создает менеджер детекции движения.
func NewManager(
	cameraRepo *postgres.CameraRepository,
	eventRepo *postgres.EventRepository,
	jobRepo *postgres.ClipJobRepository,
	clip *clipper.Clipper,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		cameraRepo: cameraRepo,
		eventRepo:  eventRepo,
		jobRepo:    jobRepo,
		clip:       clip,
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

	// snapMu защищает путь снимка текущего эпизода.
	var snapMu sync.Mutex
	snapRel := ""

	// startSnapshot асинхронно захватывает кадр живого потока в момент триггера.
	// Имя файла снимка формируется в московском времени.
	startSnapshot := func() {
		rel := fmt.Sprintf("snapshots/cam_%s/%s.jpg",
			cam.ID, time.Now().In(mskZone).Format("2006-01-02_15-04-05"))
		source := fmt.Sprintf("rtsp://127.0.0.1:8554/cam_%s", cam.ID)

		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			if err := m.clip.Snapshot(bg, source, rel); err != nil {
				m.logger.Warn("motion: snapshot failed",
					"camera_id", cam.ID,
					"error", err,
				)
				return
			}

			snapMu.Lock()
			snapRel = rel
			snapMu.Unlock()

			m.logger.Info("motion: snapshot captured",
				"camera_id", cam.ID,
				"path", rel,
			)
		}()
	}

	closeEpisode := func(end time.Time) {
		snapMu.Lock()
		snapshot := snapRel
		snapRel = ""
		snapMu.Unlock()

		payload := map[string]any{
			"started_at":   episodeStart.Format(time.RFC3339),
			"ended_at":     end.Format(time.RFC3339),
			"duration_sec": int(end.Sub(episodeStart).Seconds()),
			"source":       "onvif",
		}
		if snapshot != "" {
			payload["snapshot"] = snapshot
		}

		ev := &domain.Event{
			CameraID:   &cam.ID,
			Type:       domain.EventMotion,
			Severity:   domain.SeverityInfo,
			OccurredAt: end,
			Payload:    payload,
		}
		if err := m.eventRepo.Create(ctx, ev); err != nil {
			m.logger.Error("motion: failed to create event", "camera_id", cam.ID, "error", err)
		}

		// Окно клипа: 5 секунд предзаписи и 5 секунд постзаписи.
		job := &domain.ClipJob{
			ID:          uuid.New(),
			CameraID:    cam.ID,
			WindowStart: episodeStart.Add(-preRoll),
			WindowEnd:   end.Add(postRoll),
			Status:      "pending",
			SnapshotRel: snapshot,
		}
		if err := m.jobRepo.Create(ctx, job); err != nil {
			m.logger.Error("motion: failed to create clip job", "camera_id", cam.ID, "error", err)
		}

		m.logger.Info("motion episode closed",
			"camera_id", cam.ID,
			"started_at", episodeStart.Format(time.RFC3339),
			"ended_at", end.Format(time.RFC3339),
			"clip_window", job.WindowStart.Format(time.RFC3339)+".."+job.WindowEnd.Format(time.RFC3339),
			"snapshot", snapshot,
		)
		episodeStart = time.Time{}
	}

	subURL := ""

	for ctx.Err() == nil {
		// Корректно закрываем предыдущую подписку: камера держит лимит подписок.
		if subURL != "" {
			onvif.Unsubscribe(ctx, subURL, creds)
			subURL = ""
		}

		newSub, err := onvif.Subscribe(ctx, cam.ONVIF.Host, port, creds)
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
		subURL = newSub
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
						m.logger.Info("motion episode started",
							"camera_id", cam.ID,
							"topic", msg.Topic,
						)
						startSnapshot()
					}
					lastActive = now
				}
			}

			if !episodeStart.IsZero() && time.Since(lastActive) > episodeCooldown {
				closeEpisode(lastActive.Add(episodeCooldown))
			}
		}
	}

	// При остановке воркера освобождаем подписку на камере.
	if subURL != "" {
		onvif.Unsubscribe(context.Background(), subURL, creds)
	}

	if !episodeStart.IsZero() {
		closeEpisode(time.Now())
	}
}