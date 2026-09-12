package clipper

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// cutTimeout — предел кадрирования (все стадии).
	cutTimeout = 300 * time.Second
	// snapshotTimeout — предел захвата одного кадра.
	snapshotTimeout = 15 * time.Second
	// minClipBytes — минимальный размер корректного клипа.
	minClipBytes = 10 * 1024
	// anchorScanFPS — частота SSIM-скана при поиске кадра-якоря.
	anchorScanFPS = 2.0
	// anchorSearchBack / anchorSearchFwd — окно поиска якоря вокруг номинала.
	anchorSearchBack = 15 * time.Second
	anchorSearchFwd  = 105 * time.Second
	// anchorMinSSIM — порог доверия к найденному якорю.
	anchorMinSSIM = 0.30
)

// Clipper вырезает точные фрагменты из сегментов записи и захватывает кадры.
// Режимы работы:
//   - локальный: исполняемый файл FFmpeg из переменной FFMPEG_PATH;
//   - docker: образ CLIPPER_IMAGE (по умолчанию jrottenberg/ffmpeg:latest).
//
// Привязка ко времени: содержимое сегмента начинается с первого ключевого
// кадра после переключения, поэтому шкала файла может отставать от имени
// сегмента на величину до IDR-интервала камеры. CutAnchored устраняет это:
// кадр-якорь (снимок триггера) находится в файле через SSIM, и рез ведётся
// относительно найденной точки.
type Clipper struct {
	storageRoot string
	image       string
	ffmpegPath  string
	logger      *slog.Logger
}

// New создает кадрировщик для каталога storage.
func New(storagePath string) *Clipper {
	image := os.Getenv("CLIPPER_IMAGE")
	if image == "" {
		image = "jrottenberg/ffmpeg:latest"
	}

	ffmpeg := os.Getenv("FFMPEG_PATH")
	if ffmpeg != "" {
		if _, err := os.Stat(ffmpeg); err != nil {
			ffmpeg = ""
		}
	}

	abs, err := filepath.Abs(storagePath)
	if err != nil {
		abs = storagePath
	}

	return &Clipper{
		storageRoot: abs,
		image:       image,
		ffmpegPath:  ffmpeg,
		logger:      slog.Default(),
	}
}

// Cut вырезает фрагмент [offset, offset+duration] без привязки (fallback).
func (c *Clipper) Cut(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	return c.cut(ctx, segRel, offset, duration, clipRel, "", 0)
}

// CutAnchored вырезает фрагмент длительностью duration, привязывая начало
// к кадру-якорю: снимок тригера snapRel ищется в сегменте через SSIM,
// начало клипа = matchTime - preRoll. nominalOffset — номинальная оценка
// (центр окна поиска).
func (c *Clipper) CutAnchored(
	ctx context.Context,
	segRel, snapRel string,
	preRoll, duration, nominalOffset time.Duration,
	clipRel string,
) error {
	matchTime, ssim, err := c.findAnchor(ctx, segRel, snapRel, nominalOffset)
	if err != nil || ssim < anchorMinSSIM {
		c.logger.Warn("clip anchor not found, falling back to nominal offset",
			"segment", segRel,
			"nominal_offset_sec", fmt.Sprintf("%.3f", nominalOffset.Seconds()),
			"ssim", ssim,
			"error", err,
		)
		return c.cut(ctx, segRel, nominalOffset, duration, clipRel, "", 0)
	}

	offset := matchTime - preRoll
	if offset < 0 {
		offset = 0
	}

	c.logger.Info("clip anchor matched",
		"segment", segRel,
		"snapshot", snapRel,
		"match_time_sec", fmt.Sprintf("%.3f", matchTime.Seconds()),
		"ssim", fmt.Sprintf("%.4f", ssim),
		"final_offset_sec", fmt.Sprintf("%.3f", offset.Seconds()),
	)

	return c.cut(ctx, segRel, offset, duration, clipRel, snapRel, matchTime)
}

// findAnchor ищет в сегменте кадр, максимально совпадающий со снимком.
// Возвращает время кадра внутри файла и значение SSIM.
func (c *Clipper) findAnchor(ctx context.Context, segRel, snapRel string, nominalOffset time.Duration) (time.Duration, float64, error) {
	ctx, cancel := context.WithTimeout(ctx, cutTimeout)
	defer cancel()

	start := nominalOffset - anchorSearchBack
	if start < 0 {
		start = 0
	}
	scanLen := anchorSearchBack + anchorSearchFwd

	in := "/data/" + segRel
	snap := "/data/" + snapRel
	if c.ffmpegPath != "" {
		in = filepath.Join(c.storageRoot, filepath.FromSlash(segRel))
		snap = filepath.Join(c.storageRoot, filepath.FromSlash(snapRel))
	}

	filter := fmt.Sprintf(
		"[0:v]fps=%g,format=yuv420p[a];[1:v]format=yuv420p[b];[a][b]ssim=stats_file=-",
		anchorScanFPS,
	)

	args := c.execArgs(
		"-ss", fmt.Sprintf("%.3f", start.Seconds()),
		"-t", fmt.Sprintf("%.3f", scanLen.Seconds()),
		"-i", in,
		"-loop", "1", "-i", snap,
		"-filter_complex", filter,
		"-t", fmt.Sprintf("%.3f", scanLen.Seconds()),
		"-f", "null", "-",
	)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ffmpeg ssim scan: %w: %s", err, tail(out))
	}

	frame, val := parseSSIMStats(string(out))
	if frame < 0 {
		return 0, 0, fmt.Errorf("no ssim stats parsed")
	}

	matchTime := start + time.Duration(float64(frame)/anchorScanFPS*float64(time.Second))
	return matchTime, val, nil
}

// cut выполняет двухстадийный рез: нормализация fMP4 -> прогрессивный MP4,
// затем точная вырезка окна.
func (c *Clipper) cut(ctx context.Context, segRel string, offset, duration time.Duration, clipRel, snapRel string, matchTime time.Duration) error {
	c.logger.Info("clip cut started",
		"segment", segRel,
		"offset_sec", fmt.Sprintf("%.3f", offset.Seconds()),
		"duration_sec", fmt.Sprintf("%.3f", duration.Seconds()),
		"clip", clipRel,
	)

	var err error
	if c.ffmpegPath != "" {
		err = c.cutLocal(ctx, segRel, offset, duration, clipRel)
	} else {
		err = c.cutDocker(ctx, segRel, offset, duration, clipRel)
	}
	if err != nil {
		return err
	}

	hostOut := filepath.Join(c.storageRoot, filepath.FromSlash(clipRel))
	fi, statErr := os.Stat(hostOut)
	if statErr != nil || fi.Size() < minClipBytes {
		return fmt.Errorf("ffmpeg cut: output missing or too small: %s", hostOut)
	}

	c.logger.Info("clip cut completed", "clip", clipRel, "size_bytes", fi.Size())
	return nil
}

func normalizeArgs(in, norm string) []string {
	return []string{
		"-y",
		"-i", in,
		"-c", "copy",
		"-movflags", "+faststart",
		norm,
	}
}

func cutFromNormArgs(norm, out string, offset, duration time.Duration) []string {
	return []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", offset.Seconds()),
		"-i", norm,
		"-t", fmt.Sprintf("%.3f", duration.Seconds()),
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac", "-ar", "48000", "-ac", "1", "-b:a", "64k",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart",
		out,
	}
}

// execArgs собирает команду с учётом режима (локальный ffmpeg или docker).
func (c *Clipper) execArgs(args ...string) []string {
	if c.ffmpegPath != "" {
		return append([]string{c.ffmpegPath}, args...)
	}
	return append([]string{
		"docker", "run", "--rm",
		"-v", c.storageRoot + ":/data",
		c.image,
	}, args...)
}

func (c *Clipper) cutLocal(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	ctx, cancel := context.WithTimeout(ctx, cutTimeout)
	defer cancel()

	in := filepath.Join(c.storageRoot, filepath.FromSlash(segRel))
	out := filepath.Join(c.storageRoot, filepath.FromSlash(clipRel))
	norm := out + ".norm.mp4"
	defer os.Remove(norm)

	cmdNorm := exec.CommandContext(ctx, c.ffmpegPath, normalizeArgs(in, norm)...)
	if o, err := cmdNorm.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg normalize: %w: %s", err, tail(o))
	}

	cmdCut := exec.CommandContext(ctx, c.ffmpegPath, cutFromNormArgs(norm, out, offset, duration)...)
	if o, err := cmdCut.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg cut: %w: %s", err, tail(o))
	}
	return nil
}

func (c *Clipper) cutDocker(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	ctx, cancel := context.WithTimeout(ctx, cutTimeout)
	defer cancel()

	in := "/data/" + segRel
	out := "/data/" + clipRel
	norm := out + ".norm.mp4"
	defer os.Remove(filepath.Join(c.storageRoot, filepath.FromSlash(clipRel)+".norm.mp4"))

	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, c.execArgs(args...)[0], c.execArgs(args...)[1:]...)
		o, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, tail(o))
		}
		return nil
	}

	if err := run(normalizeArgs(in, norm)...); err != nil {
		return fmt.Errorf("ffmpeg normalize: %w", err)
	}
	if err := run(cutFromNormArgs(norm, out, offset, duration)...); err != nil {
		return fmt.Errorf("ffmpeg cut: %w", err)
	}
	return nil
}

// parseSSIMStats извлекает номер кадра с максимальным значением All.
// Формат строки: n:123 Y:0.98 U:0.99 V:0.99 All:0.98765 (12.345)
func parseSSIMStats(out string) (int, float64) {
	bestFrame, bestVal := -1, 0.0

	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "n:") {
			continue
		}
		fields := strings.Fields(line)
		var n int
		var all float64
		for _, f := range fields {
			if strings.HasPrefix(f, "n:") {
				n, _ = strconv.Atoi(strings.TrimPrefix(f, "n:"))
			}
			if strings.HasPrefix(f, "All:") {
				all, _ = strconv.ParseFloat(strings.TrimPrefix(f, "All:"), 64)
			}
		}
		if all > bestVal {
			bestVal, bestFrame = all, n
		}
	}
	return bestFrame, bestVal
}

// Snapshot захватывает один кадр из потока sourceURL и сохраняет
// в rel (путь относительно storageRoot, в slash-формате).
func (c *Clipper) Snapshot(ctx context.Context, sourceURL string, rel string) error {
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()

	out := filepath.Join(c.storageRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}

	if c.ffmpegPath != "" {
		args := []string{
			"-y", "-rtsp_transport", "tcp",
			"-i", sourceURL,
			"-frames:v", "1", "-q:v", "2",
			out,
		}
		cmd := exec.CommandContext(ctx, c.ffmpegPath, args...)
		if o, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ffmpeg snapshot: %w: %s", err, tail(o))
		}
		return nil
	}

	url := strings.Replace(sourceURL, "://127.0.0.1", "://host.docker.internal", 1)
	url = strings.Replace(url, "://localhost", "://host.docker.internal", 1)

	args := []string{
		"run", "--rm",
		"-v", c.storageRoot + ":/data",
		c.image,
		"-y", "-rtsp_transport", "tcp",
		"-i", url,
		"-frames:v", "1", "-q:v", "2",
		"/data/" + rel,
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	if o, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg snapshot: %w: %s", err, tail(o))
	}
	return nil
}

// HostPath возвращает абсолютный путь файла rel внутри storage на хосте.
func (c *Clipper) HostPath(rel string) string {
	return filepath.Join(c.storageRoot, filepath.FromSlash(rel))
}

func tail(out []byte) string {
	s := strings.TrimSpace(string(out))
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	return s
}