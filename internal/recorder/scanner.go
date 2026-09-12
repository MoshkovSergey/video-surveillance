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

	"gitverse.ru/cataclysm78/video-surveillance/internal/clipper"
	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/notify"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

const (
	// keepBufferSegments — сколько завершённых сегментов буфера режима
	// «по движению» хранить на диске (3 сегмента по 5 минут = 15 минут).
	keepBufferSegments = 3

	// clipJobTTL — срок, после которого задача без сегментов помечается failed.
	clipJobTTL = 2 * time.Hour

	// preRoll — предзапись до триггера (должно совпадать с motion.preRoll).
	preRoll = 10 * time.Second

	// postRollWait — запас после WindowEnd, чтобы MediaMTX дописал хвост на диск.
	postRollWait = 3 * time.Second
)

// mskZone — московское время (UTC+3, без сезонных переходов) для имён файлов.
var mskZone = time.FixedZone("MSK", 3*60*60)

// errCameraMissing означает, что камера удалена из базы,
// а её каталог с записями остался на диске.
var errCameraMissing = errors.New("camera no longer exists in database")

// Scanner периодически сканирует каталог сегментов MediaMTX,
// синхронизирует метаданные, ротирует буфер и вырезает клипы движения.
type Scanner struct {
	repo       *postgres.RecordingRepository
	cameraRepo *postgres.CameraRepository
	jobRepo    *postgres.ClipJobRepository
	notifier   *notify.Notifier
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
	notifier *notify.Notifier,
	root string,
	interval time.Duration,
	logger *slog.Logger,
) *Scanner {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	storageDir := filepath.Dir(absRoot)
	absStorage, err := filepath.Abs(storageDir)
	if err != nil {
		absStorage = storageDir
	}

	return &Scanner{
		repo:       repo,
		cameraRepo: cameraRepo,
		jobRepo:    jobRepo,
		notifier:   notifier,
		root:       absRoot,
		relBase:    filepath.ToSlash(absStorage),
		interval:   interval,
		logger:     logger,
		clip:       clipper.New(absStorage),
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

		if n, err := s.repo.DeleteStaleOpenSegments(ctx, cameraID); err != nil {
			s.logger.Warn("failed to cleanup stale open segments", "camera_id", cameraID, "error", err)
		} else if n > 0 {
			s.logger.Info("removed stale open recording rows", "camera_id", cameraID, "count", n)
		}
		if n, err := s.repo.DeleteOrphanPaths(ctx, cameraID, paths); err != nil {
			s.logger.Warn("failed to cleanup orphan recording paths", "camera_id", cameraID, "error", err)
		} else if n > 0 {
			s.logger.Info("removed orphan recording rows", "camera_id", cameraID, "count", n)
		}

		existing = append(existing, paths...)
	}

	s.processClipJobs(ctx)
	s.cleanupMotionBuffers(ctx)
	s.pruneSnapshots()

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
		triggerAt := job.WindowStart.Add(preRoll)

		// Ждём, пока окно полностью запишется на диск.
		if time.Now().Before(job.WindowEnd.Add(postRollWait)) {
			continue
		}

		open, err := s.repo.HasOpenOverlapping(ctx, job.CameraID, triggerAt, triggerAt)
		if err != nil {
			s.logger.Error("clip job: open segment check failed", "job_id", job.ID, "error", err)
			continue
		}
		if open {
			continue
		}

		segs, err := s.repo.ListSourceSegments(ctx, job.CameraID, job.WindowStart, job.WindowEnd)
		if err != nil {
			s.logger.Error("clip job: failed to list source segments", "job_id", job.ID, "error", err)
			continue
		}
		if len(segs) == 0 {
			if time.Since(job.CreatedAt) > clipJobTTL {
				_ = s.jobRepo.SetStatus(ctx, job.ID, "failed", "covering segments not found")

				s.notifier.NotifyMotionFallback(ctx, notify.MotionClipInfo{
					CameraID:    job.CameraID,
					SnapshotRel: job.SnapshotRel,
					WindowStart: job.WindowStart,
					WindowEnd:   job.WindowEnd,
					DurationSec: int(job.WindowEnd.Sub(job.WindowStart).Seconds()),
				}, "сегменты эпизода не найдены в буфере")
			}
			continue
		}

		clipRel, snapRel, err := s.buildMotionClip(ctx, job, segs, triggerAt)
		if err != nil {
			s.logger.Error("clip job failed", "job_id", job.ID, "error", err)
			_ = s.jobRepo.SetStatus(ctx, job.ID, "failed", err.Error())

			s.notifier.NotifyMotionFallback(ctx, notify.MotionClipInfo{
				CameraID:    job.CameraID,
				SnapshotRel: job.SnapshotRel,
				WindowStart: job.WindowStart,
				WindowEnd:   job.WindowEnd,
				DurationSec: int(job.WindowEnd.Sub(job.WindowStart).Seconds()),
			}, err.Error())
			continue
		}

		if snapRel != "" {
			job.SnapshotRel = snapRel
		}

		_ = s.jobRepo.SetStatus(ctx, job.ID, "done", "")
		s.logger.Info("clip job completed",
			"job_id", job.ID,
			"window_start", job.WindowStart.Format(time.RFC3339),
			"window_end", job.WindowEnd.Format(time.RFC3339),
			"trigger_at", triggerAt.Format(time.RFC3339),
			"clip", clipRel,
			"snapshot", job.SnapshotRel,
		)

		s.notifier.NotifyMotionClip(ctx, notify.MotionClipInfo{
			CameraID:    job.CameraID,
			ClipRel:     clipRel,
			SnapshotRel: job.SnapshotRel,
			WindowStart: job.WindowStart,
			WindowEnd:   job.WindowEnd,
			DurationSec: int(job.WindowEnd.Sub(job.WindowStart).Seconds()),
		})
	}
}

// buildMotionClip вырезает один клип вокруг триггера и снимок ИЗ ТОГО ЖЕ
// сегмента на смещении триггера — кадр гарантированно совпадает с видео.
func (s *Scanner) buildMotionClip(
	ctx context.Context,
	job domain.ClipJob,
	segs []domain.Recording,
	triggerAt time.Time,
) (clipRel, snapRel string, err error) {
	type candidate struct {
		seg          domain.Recording
		segRel       string
		contentStart time.Time
		contentEnd   time.Time
		offset       time.Duration
		duration     time.Duration
		triggerOff   time.Duration
	}

	var best *candidate

	for _, seg := range segs {
		if seg.EndedAt == nil {
			continue
		}

		srcHost, err := s.hostPath(seg.StoragePath)
		if err != nil {
			s.logger.Warn("clip job: resolve source path failed",
				"path", seg.StoragePath,
				"error", err,
			)
			continue
		}
		fi, statErr := os.Stat(srcHost)
		if statErr != nil || fi.Size() == 0 {
			s.logger.Warn("clip job: source segment missing or empty", "path", srcHost)
			continue
		}

		// Истинное начало содержимого: время создания файла.
		// Допускаем его только в правдоподобном диапазоне [имя, конец сегмента).
		contentStart := seg.StartedAt
		contentEnd := *seg.EndedAt

		if ct, ok := fileCreationTime(srcHost); ok {
			if !ct.Before(seg.StartedAt) && ct.Before(*seg.EndedAt) {
				contentStart = ct
			}
		}

		s.logger.Info("clip source mapping",
			"segment", seg.StoragePath,
			"name_time", seg.StartedAt.Format(time.RFC3339),
			"content_start", contentStart.Format(time.RFC3339),
			"delay_sec", fmt.Sprintf("%.1f", contentStart.Sub(seg.StartedAt).Seconds()),
		)

		// Триггер должен попадать в содержимое сегмента.
		if triggerAt.Before(contentStart) || !triggerAt.Before(contentEnd) {
			s.logger.Info("clip source skip (trigger outside content)",
				"segment", seg.StoragePath,
				"content_start", contentStart.Format(time.RFC3339),
				"content_end", contentEnd.Format(time.RFC3339),
				"trigger_at", triggerAt.Format(time.RFC3339),
			)
			continue
		}

		winStart := job.WindowStart
		if winStart.Before(contentStart) {
			winStart = contentStart
		}
		winEnd := job.WindowEnd
		if winEnd.After(contentEnd) {
			winEnd = contentEnd
		}
		if !winEnd.After(winStart) {
			continue
		}

		c := candidate{
			seg:          seg,
			segRel:       s.relFromStorage(seg.StoragePath),
			contentStart: contentStart,
			contentEnd:   contentEnd,
			offset:       winStart.Sub(contentStart),
			duration:     winEnd.Sub(winStart),
			triggerOff:   triggerAt.Sub(contentStart),
		}

		s.logger.Info("clip source mapping",
			"segment", seg.StoragePath,
			"name_time", seg.StartedAt.Format(time.RFC3339),
			"content_start", contentStart.Format(time.RFC3339),
			"content_end", contentEnd.Format(time.RFC3339),
			"trigger_at", triggerAt.Format(time.RFC3339),
			"trigger_offset_sec", fmt.Sprintf("%.1f", c.triggerOff.Seconds()),
			"cut_offset_sec", fmt.Sprintf("%.1f", c.offset.Seconds()),
			"cut_duration_sec", fmt.Sprintf("%.1f", c.duration.Seconds()),
			"name_delay_sec", fmt.Sprintf("%.1f", contentStart.Sub(seg.StartedAt).Seconds()),
		)

		best = &c
		break // сегменты отсортированы по started_at; первый подходящий — нужный
	}

	if best == nil {
		return "", "", errors.New("no segment covers motion trigger")
	}

	clipRel = fmt.Sprintf("clips/cam_%s/%s_%s.mp4",
		job.CameraID,
		triggerAt.In(mskZone).Format("2006-01-02_15-04-05"),
		job.ID.String()[:8],
	)
	hostClip := filepath.Join(s.relBase, filepath.FromSlash(clipRel))
	if err := os.MkdirAll(filepath.Dir(hostClip), 0o755); err != nil {
		return "", "", err
	}

	if best.segRel == clipRel {
		return "", "", errors.New("clip path collides with source")
	}

	needCut := true
	if fi, e := os.Stat(hostClip); e == nil && fi.Size() > 0 {
		needCut = false
	}

	if needCut {
		if err := s.clip.Cut(ctx, best.segRel, best.offset, best.duration, clipRel); err != nil {
			return "", "", err
		}
	}

	info, err := os.Stat(hostClip)
	if err != nil {
		return "", "", err
	}

	ended := best.contentStart.Add(best.offset + best.duration)
	started := best.contentStart.Add(best.offset)
	rec := &domain.Recording{
		CameraID:    job.CameraID,
		StartedAt:   started,
		EndedAt:     &ended,
		StoragePath: filepath.ToSlash(hostClip),
		SizeBytes:   info.Size(),
		Kept:        true,
	}
	if err := s.repo.InsertKept(ctx, rec); err != nil {
		return "", "", err
	}

	// Снимок из того же сегмента на смещении триггера — тот же кадр, что в клипе.
	snapRel = fmt.Sprintf("snapshots/cam_%s/%s_%s.jpg",
		job.CameraID,
		triggerAt.In(mskZone).Format("2006-01-02_15-04-05"),
		job.ID.String()[:8],
	)

	return clipRel, snapRel, nil
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// hostPath приводит путь из БД к абсолютному файлу на диске.
func (s *Scanner) hostPath(storagePath string) (string, error) {
	p := filepath.Clean(filepath.FromSlash(storagePath))
	if filepath.IsAbs(p) {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	candidates := []string{
		filepath.Join(filepath.FromSlash(s.relBase), p),
		p,
	}
	slash := filepath.ToSlash(p)
	if i := strings.Index(slash, "recordings/"); i >= 0 {
		candidates = append(candidates, filepath.Join(filepath.FromSlash(s.relBase), filepath.FromSlash(slash[i:])))
	}
	base := filepath.Base(filepath.FromSlash(s.relBase))
	if strings.HasPrefix(slash, base+"/") {
		candidates = append(candidates, filepath.Join(filepath.FromSlash(s.relBase), filepath.FromSlash(strings.TrimPrefix(slash, base+"/"))))
	}

	for _, c := range candidates {
		if abs, err := filepath.Abs(c); err == nil {
			c = abs
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("file not found for storage path %s", storagePath)
}

func (s *Scanner) relFromStorage(storagePath string) string {
	host, err := s.hostPath(storagePath)
	if err != nil {
		slash := filepath.ToSlash(storagePath)
		if i := strings.Index(slash, "recordings/"); i >= 0 {
			return slash[i:]
		}
		prefix := s.relBase + "/"
		if strings.HasPrefix(strings.ToLower(slash), strings.ToLower(prefix)) {
			return slash[len(prefix):]
		}
		return slash
	}
	slashHost := filepath.ToSlash(host)
	prefix := s.relBase + "/"
	if len(slashHost) > len(prefix) && strings.EqualFold(slashHost[:len(prefix)], prefix) {
		return slashHost[len(prefix):]
	}
	if i := strings.Index(slashHost, "recordings/"); i >= 0 {
		return slashHost[i:]
	}
	return filepath.Base(host)
}

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
		skipped := 0
		for _, row := range rows[keepBufferSegments:] {
			segEnd := *row.EndedAt
			needed, err := s.jobRepo.HasPendingJobOverlapping(ctx, cam.ID, row.StartedAt, segEnd)
			if err != nil {
				s.logger.Error("cleanup: pending job check failed",
					"recording_id", row.ID,
					"error", err,
				)
				continue
			}
			if needed {
				skipped++
				continue
			}

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

		if deleted > 0 || skipped > 0 {
			s.logger.Info("motion buffer trimmed",
				"camera_id", cam.ID,
				"kept", keepBufferSegments,
				"deleted", deleted,
				"skipped_for_pending_clips", skipped,
			)
		}
	}
}

func (s *Scanner) pruneSnapshots() {
	root := filepath.Join(filepath.Dir(s.root), "snapshots")
	camDirs, err := os.ReadDir(root)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	deleted := 0

	for _, dir := range camDirs {
		if !dir.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, dir.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "_") {
				continue
			}
			info, err := f.Info()
			if err != nil || info.IsDir() {
				continue
			}
			if info.ModTime().Before(cutoff) {
				if err := os.Remove(filepath.Join(root, dir.Name(), f.Name())); err == nil {
					deleted++
				}
			}
		}
	}

	if deleted > 0 {
		s.logger.Info("motion snapshots pruned", "deleted", deleted)
	}
}

type segment struct {
	path      string
	startedAt time.Time
	size      int64
}

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

		// MediaMTX без TZ пишет имена в UTC.
		startedAt, err := time.ParseInLocation("2006-01-02_15-04-05", strings.TrimSuffix(e.Name(), ".mp4"), time.UTC)
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
		if abs, err := filepath.Abs(seg.path); err == nil {
			storagePath = filepath.ToSlash(abs)
		}
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

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}
