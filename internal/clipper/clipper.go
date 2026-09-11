package clipper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// cutTimeout — предел работы одного FFmpeg-кадрирования.
	cutTimeout = 120 * time.Second
	// snapshotTimeout — предел захвата одного кадра.
	snapshotTimeout = 15 * time.Second
)

// Clipper вырезает точные фрагменты из сегментов записи и захватывает кадры.
// Режимы работы:
//   - локальный: исполняемый файл FFmpeg из переменной FFMPEG_PATH;
//   - docker: образ CLIPPER_IMAGE (по умолчанию jrottenberg/ffmpeg:latest).
type Clipper struct {
	storageRoot string
	image       string
	ffmpegPath  string
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

	return &Clipper{storageRoot: abs, image: image, ffmpegPath: ffmpeg}
}

// Cut вырезает фрагмент [offset, offset+duration] из сегмента segRel
// и пишет его в clipRel (пути относительно storageRoot, в slash-формате).
func (c *Clipper) Cut(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	if c.ffmpegPath != "" {
		return c.cutLocal(ctx, segRel, offset, duration, clipRel)
	}
	return c.cutDocker(ctx, segRel, offset, duration, clipRel)
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

	// В контейнере localhost недоступен: адресуем хост Docker Desktop.
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

// cutLocal кадрирует локальным ffmpeg.exe с перекодированием в H.264.
func (c *Clipper) cutLocal(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	ctx, cancel := context.WithTimeout(ctx, cutTimeout)
	defer cancel()

	in := filepath.Join(c.storageRoot, filepath.FromSlash(segRel))
	out := filepath.Join(c.storageRoot, filepath.FromSlash(clipRel))

	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", offset.Seconds()),
		"-i", in,
		"-t", fmt.Sprintf("%.3f", duration.Seconds()),
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-c:a", "aac", "-b:a", "64k",
		"-movflags", "+faststart",
		out,
	}

	cmd := exec.CommandContext(ctx, c.ffmpegPath, args...)
	out2, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg cut: %w: %s", err, tail(out2))
	}
	return nil
}

// cutDocker кадрирует в контейнере с образом FFmpeg.
func (c *Clipper) cutDocker(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	ctx, cancel := context.WithTimeout(ctx, cutTimeout)
	defer cancel()

	args := []string{
		"run", "--rm",
		"-v", c.storageRoot + ":/data",
		c.image,
		"-y",
		"-ss", fmt.Sprintf("%.3f", offset.Seconds()),
		"-i", "/data/" + segRel,
		"-t", fmt.Sprintf("%.3f", duration.Seconds()),
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-c:a", "aac", "-b:a", "64k",
		"-movflags", "+faststart",
		"/data/" + clipRel,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg cut: %w: %s", err, tail(out))
	}
	return nil
}

func tail(out []byte) string {
	s := strings.TrimSpace(string(out))
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	return s
}