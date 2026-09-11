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

// cutTimeout — предел работы одного FFmpeg-кадрирования.
const cutTimeout = 120 * time.Second

// Clipper вырезает точные фрагменты из сегментов записи
// с помощью FFmpeg, запущенного в Docker-контейнере.
type Clipper struct {
	storageRoot string // абсолютный путь к каталогу storage на хосте
	image       string
}

// New создает кадрировщик. Образец можно задать переменной CLIPPER_IMAGE.
func New(storagePath string) *Clipper {
	image := os.Getenv("CLIPPER_IMAGE")
	if image == "" {
		image = "jrottenberg/ffmpeg:latest"
	}

	abs, err := filepath.Abs(storagePath)
	if err != nil {
		abs = storagePath
	}

	return &Clipper{storageRoot: abs, image: image}
}

// Cut вырезает фрагмент [offset, offset+duration] из сегмента segRel
// и пишет его в clipRel (пути относительно storageRoot, в slash-формате).
// Перекодирование в H.264 обеспечивает покадровую точность границ.
func (c *Clipper) Cut(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
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
