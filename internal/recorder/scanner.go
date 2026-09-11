package recorder

import (
	"context"
	"errors"
	"fmt"
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
	"gitverse.ru/cataclysm78/video-surveillance/internal/clipper"
)

// keepBufferSegments — сколько завершённых сегментов буфера режима
// «по движению» хранить на диске (3 сегмента по 5 минут = 15 минут).
const keepBufferSegments = 3

// clipJobTTL — срок, после которого задача без сегментов помечается failed.
const clipJobTTL = 2 * time.Hour

// errCameraMissing означает, что камера удалена из базы,
// а её каталог с записями остался на диске.
var errCameraMissing = errors.New("camera no longer exists in database")

// Scanner периодически сканирует каталог сегментов MediaMTX,
// синхронизирует метаданные, ротирует буфер и вырезает клипы движения.
type Scanner struct {
	repo       *postgres.RecordingRepository
	cameraRepo *postgres.CameraRepository
	jobRepo    *postgres.ClipJobRepository
	root       string
	relBase    string
	interval   time.Duration
	logger     *slog.Logger
	clip       *clipper.Clipper

	skipped map[string]bool
}

// NewScanner создает сканер каталога записей.
func NewScanner(
	repo *postgres.RecordingRepository,
	cameraRepo *postgres.CameraRepository,
	jobRepo *postgres.ClipJobRepository,
	root string,
	interval time.Duration,
	logger *slog.Logger,
) *Scanner {
	storageDir := filepath.Dir(root)

	return &Scanner{
		repo:       repo,
		cameraRepo: cameraRepo,
		jobRepo:    jobRepo,
		root:       root,
		relBase:    filepath.ToSlash(filepath.Clean(storageDir)),
		interval:   interval,
		logger:     logger,
		clip:       clipper.New(storageDir),
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

	// Ротация буфера режима «по движению»: храним 3 последних сегмента.
	s.cleanupMotionBuffers(ctx)

	// Кадрирование точных фрагментов по эпизодам движения.
	s.processClipJobs(ctx)

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

// processClipJobs выполняет задачи вырезания точных фрагментов.
func (s *Scanner) processClipJobs(ctx context.Context) {
	jobs, err := s.jobRepo.ListPending(ctx)
	if err != nil {
		s.logger.Error("failed to list clip jobs", "error", err)
		return
	}

	for _, job := range jobs {
		open, err := s.recOpenOverlapping(ctx, job)
		if err != nil {
			s.logger.Error("clip job: open segment check failed", "job_id", job.ID, "error", err)
			continue
		}
		if open {
			// Покрывающий сегмент ещё пишется — ждём его закрытия.
			continue
		}

		segs, err := s.repo.ListOverlappingClosed(ctx, job.CameraID, job.WindowStart, job.WindowEnd)
		if err != nil {
			s.logger.Error("clip job: failed to list segments", "job_id", job.ID, "error", err)
			continue
		}
		if len(segs) == 0 {
			if time.Since(job.CreatedAt) > clipJobTTL {
				_ = s.jobRepo.SetStatus(ctx, job.ID, "failed", "covering segments not found")
			}
			continue
		}

		if err := s.buildClips(ctx, job, segs); err != nil {
			s.logger.Error("clip job failed", "job_id", job.ID, "error", err)
			_ = s.jobRepo.SetStatus(ctx, job.ID, "failed", err.Error())
			continue
		}

		_ = s.jobRepo.SetStatus(ctx, job.ID, "done", "")
		s.logger.Info("clip job completed",
			"job_id", job.ID,
			"window_start", job.WindowStart.Format(time.RFC3339),
			"window_end", job.WindowEnd.Format(time.RFC3339),
		)
	}
}

func (s *Scanner) recOpenOverlapping(ctx context.Context, job domain.ClipJob) (bool, error) {
	return s.repo.HasOpenOverlapping(ctx, job.CameraID, job.WindowStart, job.WindowEnd)
}

// buildClips вырезает по одному клипу из каждого покрывающего сегмента.
func (s *Scanner) buildClips(ctx context.Context, job domain.ClipJob, segs []domain.Recording) error {
	for _, seg := range segs {
		if seg.EndedAt == nil {
			continue
		}
		segStart := seg.StartedAt
		segEnd := *seg.EndedAt

		winStart := job.WindowStart
		if winStart.Before(segStart) {
			winStart = segStart
		}
		winEnd := job.WindowEnd
		if winEnd.After(segEnd) {
			winEnd = segEnd
		}
		if !winEnd.After(winStart) {
			continue
		}

		offset := winStart.Sub(segStart)
		duration := winEnd.Sub(winStart)

		clipRel := fmt.Sprintf("clips/cam_%s/%s.mp4",
			job.CameraID, winStart.UTC().Format("2006-01-02_15-04-05"))
		hostClip := filepath.Join(s.relBase, filepath.FromSlash(clipRel))

		if err := os.MkdirAll(filepath.Dir(hostClip), 0o755); err != nil {
			return err
		}

		segRel := strings.TrimPrefix(seg.StoragePath, s.relBase+"/")
		if err := s.clip.Cut(ctx, segRel, offset, duration, clipRel); err != nil {
			return err
		}

		info, err := os.Stat(hostClip)
		if err != nil {
			return err
		}

		ended := winEnd
		rec := &domain.Recording{
			CameraID:    job.CameraID,
			StartedAt:   winStart,
			EndedAt:     &ended,
			StoragePath: filepath.ToSlash(hostClip),
			SizeBytes:   info.Size(),
			Kept:        true,
		}
		if err := s.repo.InsertKept(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}

// cleanupMotionBuffers удаляет сегменты буфера сверх лимита keepBufferSegments.
// Помеченные kept (клипы и эпизоды) не удаляются никогда.
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
