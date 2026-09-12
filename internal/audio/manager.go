package audio

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// restartDelay — пауза перед перезапуском упавшего транскодера.
	restartDelay = 3 * time.Second
	// stopTimeout — предел корректной остановки контейнера.
	stopTimeout = 8 * time.Second
	// cleanupTimeout — предел удаления старого контейнера.
	cleanupTimeout = 5 * time.Second
)

// Manager управляет процессами транскодинга звука G.711 -> AAC
// для живого просмотра в браузере.
// Режимы:
//   - локальный: FFMPEG_PATH задан и файл существует;
//   - docker: образ CLIPPER_IMAGE (по умолчанию jrottenberg/ffmpeg:latest).
type Manager struct {
	mu     sync.Mutex
	procs  map[uuid.UUID]*proc
	ffmpeg string
	image  string
	logger *slog.Logger
}

type proc struct {
	cancel context.CancelFunc
	done   chan struct{}
	// docker — имя контейнера в docker-режиме (пусто в локальном).
	docker string
}

// NewManager создает менеджер звуковых сайкаров.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}

	ffmpeg := os.Getenv("FFMPEG_PATH")
	if ffmpeg != "" {
		if _, err := os.Stat(ffmpeg); err != nil {
			ffmpeg = ""
		}
	}

	image := os.Getenv("CLIPPER_IMAGE")
	if image == "" {
		image = "jrottenberg/ffmpeg:latest"
	}

	return &Manager{
		procs:  make(map[uuid.UUID]*proc),
		ffmpeg: ffmpeg,
		image:  image,
		logger: logger,
	}
}

// Available сообщает, доступен ли любой режим транскодинга.
func (m *Manager) Available() bool { return m.ffmpeg != "" || m.image != "" }

// Running сообщает, запущен ли сайкар для камеры.
func (m *Manager) Running(cameraID uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.procs[cameraID]
	return ok
}

func containerName(cameraID uuid.UUID) string {
	return "vs-audio-" + cameraID.String()
}

// Start запускает транскодер звука камеры (идемпотентно).
func (m *Manager) Start(cameraID uuid.UUID, sourceRTSP string) error {
	m.mu.Lock()
	if _, ok := m.procs[cameraID]; ok {
		m.mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &proc{cancel: cancel, done: make(chan struct{})}
	if m.ffmpeg == "" {
		p.docker = containerName(cameraID)
	}
	m.procs[cameraID] = p
	m.mu.Unlock()

	mode := "local"
	if p.docker != "" {
		mode = "docker"
	}

	go m.supervise(ctx, p, cameraID, sourceRTSP)

	m.logger.Info("audio transcoder started",
		"camera_id", cameraID,
		"mode", mode,
	)
	return nil
}

// supervise держит транскодер живым до отмены.
func (m *Manager) supervise(ctx context.Context, p *proc, cameraID uuid.UUID, sourceRTSP string) {
	defer close(p.done)

	path := "cam_" + cameraID.String() + "_audio"
	target := "rtsp://127.0.0.1:8554/" + path
	if p.docker != "" {
		// Из контейнера хост виден как host.docker.internal.
		target = "rtsp://host.docker.internal:8554/" + path

		// Убираем возможный старый контейнер с тем же именем.
		cctx, ccancel := context.WithTimeout(context.Background(), cleanupTimeout)
		_ = exec.CommandContext(cctx, "docker", "rm", "-f", p.docker).Run()
		ccancel()
	}

	ffArgs := []string{
		"-rtsp_transport", "tcp",
		"-i", sourceRTSP,
		"-c:v", "copy",
		"-c:a", "aac", "-ar", "48000", "-ac", "1", "-b:a", "64k",
		"-f", "rtsp",
		"-rtsp_transport", "tcp",
		target,
	}

	for ctx.Err() == nil {
		var cmd *exec.Cmd
		if p.docker != "" {
			args := append([]string{"run", "--rm", "--name", p.docker, m.image}, ffArgs...)
			cmd = exec.CommandContext(ctx, "docker", args...)
		} else {
			cmd = exec.CommandContext(ctx, m.ffmpeg, ffArgs...)
		}

		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			break
		}

		m.logger.Warn("audio transcoder exited, restarting",
			"camera_id", cameraID,
			"error", err,
			"output", tail(out),
		)

		select {
		case <-ctx.Done():
		case <-time.After(restartDelay):
		}
	}

	// В docker-режиме убийство CLI не останавливает контейнер — гасим явно.
	if p.docker != "" {
		sctx, scancel := context.WithTimeout(context.Background(), stopTimeout)
		_ = exec.CommandContext(sctx, "docker", "stop", "-t", "2", p.docker).Run()
		scancel()
	}
}

// Stop останавливает транскодер звука камеры.
func (m *Manager) Stop(cameraID uuid.UUID) {
	m.mu.Lock()
	p, ok := m.procs[cameraID]
	if ok {
		delete(m.procs, cameraID)
	}
	m.mu.Unlock()

	if !ok {
		return
	}

	p.cancel()
	<-p.done

	m.logger.Info("audio transcoder stopped", "camera_id", cameraID)
}

func tail(out []byte) string {
	s := string(out)
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return s
}