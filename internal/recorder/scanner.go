package recorder

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

// Scanner периодически сканирует каталог сегментов MediaMTX
// и синхронизирует метаданные записей в PostgreSQL.
type Scanner struct {
	repo     *postgres.RecordingRepository
	root     string
	interval time.Duration
	logger   *slog.Logger
}

// NewScanner создает сканер каталога записей.
func NewScanner(repo *postgres.RecordingRepository, root string, interval time.Duration, logger *slog.Logger) *Scanner {
	return &Scanner{
		repo:     repo,
		root:     root,
		interval: interval,
		logger:   logger,
	}
}

// Start запускает цикл сканирования в отдельной горутине.
func (s *Scanner) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		// Первый скан сразу после старта.
		s.scan(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.scan(ctx)
			}
		}
	}()
}

func (s *Scanner) scan(ctx context.Context) {
	cameraDirs, err := os.ReadDir(s.root)
	if err != nil {
		if !os.IsNotExist(err) {
			s.logger.Error("failed to read recordings root", "root", s.root, "error", err)
		}
		return
	}

	for _, dir := range cameraDirs {
		if !dir.IsDir() {
			continue
		}

		cameraID, err := uuid.Parse(strings.TrimPrefix(dir.Name(), "cam_"))
		if err != nil {
			continue // посторонний каталог
		}

		if err := s.scanCamera(ctx, cameraID, filepath.Join(s.root, dir.Name())); err != nil {
			s.logger.Error("failed to scan camera recordings",
				"camera_id", cameraID,
				"error", err,
			)
		}
	}
}

type segment struct {
	path      string
	startedAt time.Time
	size      int64
}

func (s *Scanner) scanCamera(ctx context.Context, cameraID uuid.UUID, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	segments := make([]segment, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mp4") {
			continue
		}

		// Имена файлов MediaMTX: 2006-01-02_15-04-05.mp4 (время UTC контейнера)
		startedAt, err := time.Parse("2006-01-02_15-04-05", strings.TrimSuffix(e.Name(), ".mp4"))
		if err != nil {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		segments = append(segments, segment{
			path:      filepath.Join(dir, e.Name()),
			startedAt: startedAt,
			size:      info.Size(),
		})
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].startedAt.Before(segments[j].startedAt)
	})

	for i, seg := range segments {
		var endedAt *time.Time
		if i+1 < len(segments) {
			next := segments[i+1].startedAt
			endedAt = &next
		}

		rec := &domain.Recording{
			CameraID:    cameraID,
			StartedAt:   seg.startedAt,
			EndedAt:     endedAt,
			StoragePath: filepath.ToSlash(seg.path),
			SizeBytes:   seg.size,
		}

		if err := s.repo.Upsert(ctx, rec); err != nil {
			return err
		}
	}

	return nil
}