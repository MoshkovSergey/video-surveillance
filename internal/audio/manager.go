package audio

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
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

// Status — состояние звукового сайкара камеры.
type Status struct {
	Running   bool   `json:"running"`
	Mode      string `json:"mode"`
	LastError string `json:"last_error,omitempty"`
}

// Manager управляет процессами транскодинга звука G.711 -> AAC
// для живого просмотра в браузере. Публикуется ТОЛЬКО аудио:
// видеоплеер остаётся на основном потоке камеры.
type Manager struct {
	mu     sync.Mutex
	procs  map[uuid.UUID]*proc
	errs   map[uuid.UUID]string
	ffmpeg string
	image  string
	logger *slog.Logger
}

type proc struct {
	cancel context.CancelFunc
	done   chan struct{}
	docker string
	mode   string
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
		errs:   make(map[uuid.UUID]string),
		ffmpeg: ffmpeg,
		image:  image,
		logger: logger,
	}
}

var (
	defaultMu  sync.Mutex
	defaultMgr *Manager
)

// SetDefault регистрирует менеджер для HTTP-обработчиков.
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

// Running сообщает, запущен ли сайкар для камеры.
func (m *Manager) Running(cameraID uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.procs[cameraID]
	return ok
}

// Status возвращает состояние сайкара и последнюю ошибку.
func (m *Manager) Status(cameraID uuid.UUID) Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := Status{LastError: m.errs[cameraID]}
	if p, ok := m.procs[cameraID]; ok {
		st.Running = true
		st.Mode = p.mode
	}
	return st
}

func (m *Manager) setErr(cameraID uuid.UUID, msg string) {
	m.mu.Lock()
	m.errs[cameraID] = msg
	m.mu.Unlock()
}

func (m *Manager) clearErr(cameraID uuid.UUID) {
	m.mu.Lock()
	delete(m.errs, cameraID)
	m.mu.Unlock()
}

func containerName(cameraID uuid.UUID) string {
	return "vs-audio-" + cameraID.String()
}

// toHostAddr приводит localhost-адреса к host.docker.internal для контейнера:
// внутри контейнера 127.0.0.1 — это сам контейнер, а не хост.
func toHostAddr(url string) string {
	url = strings.Replace(url, "://127.0.0.1", "://host.docker.internal", 1)
	url = strings.Replace(url, "://localhost", "://host.docker.internal", 1)
	return url
}

// Start запускает транскодер звука камеры (идемпотентно).
func (m *Manager) Start(cameraID uuid.UUID, sourceRTSP string) error {
	m.mu.Lock()
	if _, ok := m.procs[cameraID]; ok {
		m.mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &proc{cancel: cancel, done: make(chan struct{}), mode: "local"}
	if m.ffmpeg == "" {
		p.docker = containerName(cameraID)
		p.mode = "docker"
	}
	m.procs[cameraID] = p
	m.mu.Unlock()

	m.clearErr(cameraID)
	go m.supervise(ctx, p, cameraID, sourceRTSP)

	m.logger.Info("audio transcoder started", "camera_id", cameraID, "mode", p.mode)
	return nil
}

// supervise держит транскодер живым до отмены, фиксируя ошибки.
func (m *Manager) supervise(ctx context.Context, p *proc, cameraID uuid.UUID, sourceRTSP string) {
	defer close(p.done)
	defer func() {
		m.mu.Lock()
		delete(m.procs, cameraID)
		m.mu.Unlock()
	}()

	path := "cam_" + cameraID.String() + "_audio"

	// Источник и цель: в docker-режиме адресуем хост через host.docker.internal.
	src := sourceRTSP
	target := "rtsp://127.0.0.1:8554/" + path
	if p.docker != "" {
		src = toHostAddr(sourceRTSP)
		target = "rtsp://host.docker.internal:8554/" + path

		cctx, ccancel := context.WithTimeout(context.Background(), cleanupTimeout)
		_ = exec.CommandContext(cctx, "docker", "rm", "-f", p.docker).Run()
		ccancel()
	}

	// Только аудио: видео остаётся в основном потоке камеры.
	ffArgs := []string{
		"-rtsp_transport", "tcp",
		"-i", src,
		"-vn",
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

		msg := fmt.Sprintf("транскодер завершился: %v: %s", err, tail(out))
		m.setErr(cameraID, msg)
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