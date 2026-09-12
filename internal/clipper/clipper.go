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
//
// Схема надёжного и синхронного реза:
//   - входной точный поиск (-ss до -i): при перекодировании ffmpeg декодирует
//     с ближайшего ключевого кадра и отбрасывает лишнее, точка seek одна
//     для всех дорожек — звук и видео остаются синхронными;
//   - принудительные совместимые параметры: H.264 yuv420p + AAC 48 кГц моно;
//   - никаких genpts и ручных таймскейлов: таймстампы формируются заново.
func (c *Clipper) Cut(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	if c.ffmpegPath != "" {
		return c.cutLocal(ctx, segRel, offset, duration, clipRel)
	}
	return c.cutDocker(ctx, segRel, offset, duration, clipRel)
}

// cutArgs формирует аргументы FFmpeg для кадрирования.
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
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart",
		out,
	}
}

// cutLocal кадрирует локальным ffmpeg.exe.
func (c *Clipper) cutLocal(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	ctx, cancel := context.WithTimeout(ctx, cutTimeout)
	defer cancel()

	in := filepath.Join(c.storageRoot, filepath.FromSlash(segRel))
	out := filepath.Join(c.storageRoot, filepath.FromSlash(clipRel))

	cmd := exec.CommandContext(ctx, c.ffmpegPath, cutArgs(in, out, offset, duration)...)
	out2, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg cut: %w: %s", err, tail(out2))
	}

	if fi, err := os.Stat(out); err != nil || fi.Size() < minClipBytes {
		return fmt.Errorf("ffmpeg cut: output missing or too small: %s", out)
	}
	return nil
}

// cutDocker кадрирует в контейнере с образом FFmpeg.
func (c *Clipper) cutDocker(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	ctx, cancel := context.WithTimeout(ctx, cutTimeout)
	defer cancel()

	args := append([]string{
		"run", "--rm",
		"-v", c.storageRoot + ":/data",
		c.image,
	}, cutArgs("/data/"+segRel, "/data/"+clipRel, offset, duration)...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg cut: %w: %s", err, tail(out))
	}

	hostOut := filepath.Join(c.storageRoot, filepath.FromSlash(clipRel))
	if fi, err := os.Stat(hostOut); err != nil || fi.Size() < minClipBytes {
		return fmt.Errorf("ffmpeg cut: output missing or too small: %s", hostOut)
	}
	return nil
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

func tail(out []byte) string {
	s := strings.TrimSpace(string(out))
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	return s
}