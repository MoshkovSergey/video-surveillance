package clipper

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// cutTimeout — предел кадрирования.
	cutTimeout = 300 * time.Second
	// snapshotTimeout — предел захвата одного кадра.
	snapshotTimeout = 15 * time.Second
	// minClipBytes — минимальный размер корректного клипа.
	minClipBytes = 10 * 1024
)

// Clipper вырезает точные фрагменты из сегментов записи и захватывает кадры.
// Режимы работы:
//   - локальный: исполняемый файл FFmpeg из переменной FFMPEG_PATH;
//   - docker: образ CLIPPER_IMAGE (по умолчанию jrottenberg/ffmpeg:latest).
//
// Схема реза: одностадийный вызов с перекодированием (не copy).
// Входной -ss даёт точный поиск с декодированием от ключевого кадра,
// перекодирование выравнивает таймстампы аудио и видео с нуля —
// дорожки синхронны. Флаг -copyts НЕ используется: с ним выходная шкала
// начинается с позиции реза и опция -t останавливает запись до первого кадра.
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

// Cut вырезает фрагмент [offset, offset+duration] из сегмента segRel
// и пишет его в clipRel (пути относительно storageRoot, в slash-формате).
func (c *Clipper) Cut(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	c.logger.Info("clip cut started",
		"segment", segRel,
		"offset_sec", fmt.Sprintf("%.3f", offset.Seconds()),
		"duration_sec", fmt.Sprintf("%.3f", duration.Seconds()),
		"clip", clipRel,
	)

	var out string
	var err error
	if c.ffmpegPath != "" {
		out, err = c.cutLocal(ctx, segRel, offset, duration, clipRel)
	} else {
		out, err = c.cutDocker(ctx, segRel, offset, duration, clipRel)
	}
	if err != nil {
		return err
	}

	fi, statErr := os.Stat(out)
	if statErr != nil || fi.Size() < minClipBytes {
		return fmt.Errorf("ffmpeg cut: output missing or too small: %s", out)
	}

	c.logger.Info("clip cut completed", "clip", clipRel, "size_bytes", fi.Size())
	return nil
}

// cutArgs формирует аргументы FFmpeg для одностадийного реза с перекодированием.
//   - -ss перед -i: точный поиск (декодирование от ключевого кадра до точки);
//   - -t после -i: ограничение длительности выходной шкалы (начинается с 0);
//   - перекодирование libx264+aac: таймстампы обеих дорожек стартуют с нуля,
//     звук и видео синхронны.
func cutArgs(in, out string, offset, duration time.Duration) []string {
	return []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", offset.Seconds()),
		"-i", in,
		"-t", fmt.Sprintf("%.3f", duration.Seconds()),
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac", "-ar", "48000", "-ac", "1", "-b:a", "64k",
		"-movflags", "+faststart",
		out,
	}
}

// cutLocal кадрирует локальным ffmpeg.exe; возвращает путь результата на хосте.
func (c *Clipper) cutLocal(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cutTimeout)
	defer cancel()

	in := filepath.Join(c.storageRoot, filepath.FromSlash(segRel))
	out := filepath.Join(c.storageRoot, filepath.FromSlash(clipRel))

	cmd := exec.CommandContext(ctx, c.ffmpegPath, cutArgs(in, out, offset, duration)...)
	if o, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ffmpeg cut: %w: %s", err, tail(o))
	}
	return out, nil
}

// cutDocker кадрирует в контейнере с образом FFmpeg; возвращает путь результата на хосте.
func (c *Clipper) cutDocker(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cutTimeout)
	defer cancel()

	in := "/data/" + segRel
	out := "/data/" + clipRel

	args := append(
		[]string{"run", "--rm", "-v", c.storageRoot + ":/data", c.image},
		cutArgs(in, out, offset, duration)...,
	)
	cmd := exec.CommandContext(ctx, "docker", args...)
	if o, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ffmpeg cut: %w: %s", err, tail(o))
	}
	return filepath.Join(c.storageRoot, filepath.FromSlash(clipRel)), nil
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