package recorder

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

// keepBufferSegments — сколько завершённых сегментов буфера режима
// «по движению» хранить на диске (3 сегмента по 5 минут = 15 минут).
const keepBufferSegments = 3

// errCameraMissing означает, что камера удалена из базы,
// а её каталог с записями остался на диске.
var errCameraMissing = errors.New("camera no longer exists in database")

// Scanner периодически сканирует каталог сегментов MediaMTX
// и синхронизирует метаданные записей в PostgreSQL.
type Scanner struct {
	repo       *postgres.RecordingRepository
	cameraRepo *postgres.CameraRepository
	root       string
	interval   time.Duration
	logger     *slog.Logger

	skipped map[string]bool
}

// NewScanner создает сканер каталога записей.
func NewScanner(
	repo *postgres.RecordingRepository,
	cameraRepo *postgres.CameraRepository,
	root string,
	interval time.Duration,
	logger *slog.Logger,
) *Scanner {
	return &Scanner{
		repo:       repo,
		cameraRepo: cameraRepo,
		root:       root,
		interval:   interval,
		logger:     logger,
		skipped:    make(map[string]bool),
	}
}

// Start запускает цикл сканирования в отдельной горутине.
func (s *Scanner) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

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

	existing := make([]string, 0, 64)
	scanFailed := false

	for _, dir := range cameraDirs {
		if !dir.IsDir() {
			continue
		}

		cameraID, err := uuid.Parse(strings.TrimPrefix(dir.Name(), "cam_"))
		if err != nil {
			continue
		}

		paths, err := s.scanCamera(ctx, cameraID, filepath.Join(s.root, dir.Name()))
		if err != nil {
			if errors.Is(err, errCameraMissing) {
				if !s.skipped[dir.Name()] {
					s.logger.Info("recordings directory belongs to a deleted camera; skipping it",
						"dir", dir.Name(),
					)
					s.skipped[dir.Name()] = true
				}
				continue
			}

			s.logger.Error("failed to scan camera recordings",
				"camera_id", cameraID,
				"error", err,
			)
			scanFailed = true
			continue
		}

		existing = append(existing, paths...)
	}

	// Очистка буфера режима «по движению»: храним 3 последних сегмента.
	s.cleanupMotionBuffers(ctx)

	if scanFailed {
		s.logger.Warn("skipping recordings prune due to scan errors")
		return
	}

	removed, err := s.repo.PruneMissing(ctx, existing, filepath.ToSlash(s.root)+"/%")
	if err != nil {
		s.logger.Error("failed to prune missing recordings", "error", err)
		return
	}
	if removed > 0 {
		s.logger.Info("pruned recordings missing on disk", "count", removed)
	}
}

// cleanupMotionBuffers удаляет сегменты буфера сверх лимита keepBufferSegments.
// Помеченные kept (эпизоды движения) не удаляются никогда.
func (s *Scanner) cleanupMotionBuffers(ctx context.Context) {
	cams, err := s.cameraRepo.List(ctx)
	if err != nil {
		s.logger.Error("failed to list cameras for buffer cleanup", "error", err)
		return
	}

	for _, cam := range cams {
		if cam.RecordingMode != domain.RecordingMotion {
			continue
		}

		rows, err := s.repo.ListMotionBuffer(ctx, cam.ID)
		if err != nil {
			s.logger.Error("failed to list motion buffer", "camera_id", cam.ID, "error", err)
			continue
		}

		if len(rows) <= keepBufferSegments {
			continue
		}

		deleted := 0
		for _, row := range rows[keepBufferSegments:] {
			if err := os.Remove(row.StoragePath); err != nil && !os.IsNotExist(err) {
				s.logger.Error("failed to remove buffer file",
					"path", row.StoragePath,
					"error", err,
				)
				continue
			}
			if err := s.repo.DeleteByID(ctx, row.ID); err != nil {
				s.logger.Error("failed to delete buffer row",
					"recording_id", row.ID,
					"error", err,
				)
				continue
			}
			deleted++
		}

		if deleted > 0 {
			s.logger.Info("motion buffer trimmed to last segments",
				"camera_id", cam.ID,
				"kept", keepBufferSegments,
				"deleted", deleted,
			)
		}
	}
}

type segment struct {
	path      string
	startedAt time.Time
	size      int64
}

// scanCamera синхронизирует сегменты одной камеры и возвращает
// список путей файлов, которые реально существуют на диске.
func (s *Scanner) scanCamera(ctx context.Context, cameraID uuid.UUID, dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	segments := make([]segment, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mp4") {
			continue
		}

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

	paths := make([]string, 0, len(segments))

	for i, seg := range segments {
		var endedAt *time.Time
		if i+1 < len(segments) {
			next := segments[i+1].startedAt
			endedAt = &next
		}

		storagePath := filepath.ToSlash(seg.path)
		paths = append(paths, storagePath)

		rec := &domain.Recording{
			CameraID:    cameraID,
			StartedAt:   seg.startedAt,
			EndedAt:     endedAt,
			StoragePath: storagePath,
			SizeBytes:   seg.size,
		}

		if err := s.repo.Upsert(ctx, rec); err != nil {
			if isForeignKeyViolation(err) {
				return nil, errCameraMissing
			}
			return nil, err
		}
	}

	return paths, nil
}

// isForeignKeyViolation распознаёт нарушение внешнего ключа:
// штатный признак того, что камера удалена из базы.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}