package clipper

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	cutTimeout      = 300 * time.Second
	snapshotTimeout = 20 * time.Second
	minClipBytes    = 10 * 1024
)

var (
	reDuration = regexp.MustCompile(`Duration: (\d+):(\d+):(\d+(?:\.\d+)?)`)
	reStart    = regexp.MustCompile(`start: (-?\d+(?:\.\d+)?)`)
)

// Clipper вырезает фрагменты из сегментов записи и захватывает кадры.
// Режимы: локальный FFMPEG_PATH или docker CLIPPER_IMAGE.
type Clipper struct {
	storageRoot string
	image       string
	ffmpegPath  string
	ffprobePath string
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

	ffprobe := ""
	if ffmpeg != "" {
		candidate := filepath.Join(filepath.Dir(ffmpeg), "ffprobe.exe")
		if _, err := os.Stat(candidate); err != nil {
			candidate = filepath.Join(filepath.Dir(ffmpeg), "ffprobe")
		}
		if _, err := os.Stat(candidate); err == nil {
			ffprobe = candidate
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
		ffprobePath: ffprobe,
		logger:      slog.Default(),
	}
}

// ProbeInfo — таймлайн медиафайла.
// VideoDuration важнее FormatDuration: у MediaMTX аудио часто длиннее видео
// (тишина до первого keyframe), и смещение по format duration сдвигает клип мимо движения.
type ProbeInfo struct {
	Start          time.Duration
	FormatDuration time.Duration
	VideoDuration  time.Duration
}

// ContentDuration — длительность именно видео (для привязки к кадрам).
func (p ProbeInfo) ContentDuration() time.Duration {
	if p.VideoDuration > time.Second {
		return p.VideoDuration
	}
	return p.FormatDuration
}

// Probe возвращает таймлайн файла на хосте.
func (c *Clipper) Probe(ctx context.Context, hostPath string) (ProbeInfo, error) {
	if info, err := c.probeStreams(ctx, hostPath); err == nil && info.ContentDuration() > time.Second {
		return info, nil
	}
	return c.probeFFmpeg(ctx, hostPath)
}

// Duration — длительность видео-содержимого.
func (c *Clipper) Duration(ctx context.Context, hostPath string) (time.Duration, error) {
	p, err := c.Probe(ctx, hostPath)
	return p.ContentDuration(), err
}

// Cut вырезает [offset, offset+duration] от начала видео-содержимого.
func (c *Clipper) Cut(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	if offset < 0 {
		offset = 0
	}
	if duration <= 0 {
		return fmt.Errorf("ffmpeg cut: non-positive duration")
	}

	c.logger.Info("clip cut started",
		"segment", segRel,
		"offset_sec", fmt.Sprintf("%.3f", offset.Seconds()),
		"duration_sec", fmt.Sprintf("%.3f", duration.Seconds()),
		"clip", clipRel,
	)

	hostOut := filepath.Join(c.storageRoot, filepath.FromSlash(clipRel))
	if err := os.MkdirAll(filepath.Dir(hostOut), 0o755); err != nil {
		return err
	}

	var err error
	if c.ffmpegPath != "" {
		err = c.cutLocal(ctx, segRel, offset, duration, clipRel)
	} else {
		err = c.cutDocker(ctx, segRel, offset, duration, clipRel)
	}
	if err != nil {
		return err
	}

	fi, statErr := os.Stat(hostOut)
	if statErr != nil || fi.Size() < minClipBytes {
		return fmt.Errorf("ffmpeg cut: output missing or too small: %s", hostOut)
	}

	c.logger.Info("clip cut completed", "clip", clipRel, "size_bytes", fi.Size())
	return nil
}

// cutArgs — по https://ffmpeg.org/ffmpeg.html:
// -ss до -i при транскодинге + accurate_seek (default) = быстрый и точный seek.
func cutArgs(in, out string, offset, duration time.Duration) []string {
	ss := fmt.Sprintf("%.3f", offset.Seconds())
	dd := fmt.Sprintf("%.3f", duration.Seconds())

	return []string{
		"-y",
		"-ss", ss,
		"-i", in,
		"-t", dd,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-vf", "setpts=PTS-STARTPTS",
		"-af", "asetpts=PTS-STARTPTS",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac", "-ar", "48000", "-ac", "1", "-b:a", "64k",
		"-shortest",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart",
		out,
	}
}

func frameArgs(in, out string, offset time.Duration) []string {
	args := []string{"-y"}
	if offset > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", offset.Seconds()))
	}
	args = append(args,
		"-i", in,
		"-frames:v", "1",
		"-update", "1",
		"-q:v", "2",
		out,
	)
	return args
}

func (c *Clipper) dockerVolume() string {
	return filepath.ToSlash(c.storageRoot) + ":/data"
}

func (c *Clipper) runDocker(ctx context.Context, entrypoint string, args ...string) ([]byte, error) {
	full := []string{"run", "--rm", "-v", c.dockerVolume()}
	if entrypoint != "" {
		full = append(full, "--entrypoint", entrypoint)
	}
	full = append(full, c.image)
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	return cmd.CombinedOutput()
}

func (c *Clipper) cutLocal(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	ctx, cancel := context.WithTimeout(ctx, cutTimeout)
	defer cancel()

	in := filepath.Join(c.storageRoot, filepath.FromSlash(segRel))
	out := filepath.Join(c.storageRoot, filepath.FromSlash(clipRel))

	cmd := exec.CommandContext(ctx, c.ffmpegPath, cutArgs(in, out, offset, duration)...)
	if o, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg cut: %w: %s", err, tail(o))
	}
	return nil
}

func (c *Clipper) cutDocker(ctx context.Context, segRel string, offset, duration time.Duration, clipRel string) error {
	ctx, cancel := context.WithTimeout(ctx, cutTimeout)
	defer cancel()

	in := "/data/" + segRel
	out := "/data/" + clipRel

	if o, err := c.runDocker(ctx, "", cutArgs(in, out, offset, duration)...); err != nil {
		return fmt.Errorf("ffmpeg cut: %w: %s", err, tail(o))
	}
	return nil
}

// ExtractFrame сохраняет кадр из segRel на смещении offset.
func (c *Clipper) ExtractFrame(ctx context.Context, segRel string, offset time.Duration, frameRel string) error {
	if offset < 0 {
		offset = 0
	}

	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()

	hostOut := filepath.Join(c.storageRoot, filepath.FromSlash(frameRel))
	if err := os.MkdirAll(filepath.Dir(hostOut), 0o755); err != nil {
		return err
	}

	if c.ffmpegPath != "" {
		in := filepath.Join(c.storageRoot, filepath.FromSlash(segRel))
		cmd := exec.CommandContext(ctx, c.ffmpegPath, frameArgs(in, hostOut, offset)...)
		if o, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ffmpeg frame: %w: %s", err, tail(o))
		}
	} else {
		in := "/data/" + segRel
		out := "/data/" + frameRel
		if o, err := c.runDocker(ctx, "", frameArgs(in, out, offset)...); err != nil {
			return fmt.Errorf("ffmpeg frame: %w: %s", err, tail(o))
		}
	}

	fi, err := os.Stat(hostOut)
	if err != nil || fi.Size() < 1024 {
		return fmt.Errorf("ffmpeg frame: output missing or too small: %s", hostOut)
	}
	return nil
}

func (c *Clipper) probeStreams(ctx context.Context, hostPath string) (ProbeInfo, error) {
	videoDur, err := c.probeDuration(ctx, hostPath, true)
	if err != nil {
		return ProbeInfo{}, err
	}
	formatDur, ferr := c.probeDuration(ctx, hostPath, false)
	if ferr != nil || formatDur < time.Second {
		formatDur = videoDur
	}
	return ProbeInfo{
		FormatDuration: formatDur,
		VideoDuration:  videoDur,
	}, nil
}

// probeDuration: videoOnly=true → длительность первого видео-потока;
// иначе — format duration (часто = аудио у MediaMTX).
func (c *Clipper) probeDuration(ctx context.Context, hostPath string, videoOnly bool) (time.Duration, error) {
	abs, err := c.resolveHost(hostPath)
	if err != nil {
		return 0, err
	}

	var args []string
	if videoOnly {
		args = []string{
			"-v", "error",
			"-select_streams", "v:0",
			"-show_entries", "stream=duration",
			"-of", "default=noprint_wrappers=1:nokey=1",
		}
	} else {
		args = []string{
			"-v", "error",
			"-show_entries", "format=duration",
			"-of", "default=noprint_wrappers=1:nokey=1",
		}
	}

	var out []byte
	if c.ffprobePath != "" {
		cmd := exec.CommandContext(ctx, c.ffprobePath, append(args, abs)...)
		out, err = cmd.CombinedOutput()
	} else {
		rel, relErr := c.relUnderStorage(abs)
		if relErr != nil {
			return 0, relErr
		}
		out, err = c.runDocker(ctx, "ffprobe", append(args, "/data/"+rel)...)
	}
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w: %s", err, tail(out))
	}

	sec, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || sec <= 0 {
		return 0, fmt.Errorf("ffprobe: bad duration %q", strings.TrimSpace(string(out)))
	}
	return time.Duration(sec * float64(time.Second)), nil
}

func (c *Clipper) probeFFmpeg(ctx context.Context, hostPath string) (ProbeInfo, error) {
	abs, err := c.resolveHost(hostPath)
	if err != nil {
		return ProbeInfo{}, err
	}

	var out []byte
	if c.ffmpegPath != "" {
		cmd := exec.CommandContext(ctx, c.ffmpegPath, "-hide_banner", "-i", abs)
		out, _ = cmd.CombinedOutput()
	} else {
		rel, relErr := c.relUnderStorage(abs)
		if relErr != nil {
			return ProbeInfo{}, relErr
		}
		out, _ = c.runDocker(ctx, "", "-hide_banner", "-i", "/data/"+rel)
	}

	start, dur, ok := parseFFmpegProbe(out)
	if !ok {
		return ProbeInfo{}, fmt.Errorf("ffmpeg probe: cannot parse duration: %s", tail(out))
	}
	return ProbeInfo{Start: start, FormatDuration: dur, VideoDuration: dur}, nil
}

// resolveHost приводит путь к абсолютному на хосте.
func (c *Clipper) resolveHost(hostPath string) (string, error) {
	p := filepath.Clean(filepath.FromSlash(hostPath))
	if filepath.IsAbs(p) {
		return p, nil
	}
	// Относительный путь из БД: "storage/recordings/..." или "recordings/..."
	joined := filepath.Join(c.storageRoot, p)
	if _, err := os.Stat(joined); err == nil {
		return joined, nil
	}
	// Если префикс уже содержит имя каталога storage — срезаем и пробуем снова.
	base := filepath.Base(c.storageRoot)
	slash := filepath.ToSlash(p)
	if strings.HasPrefix(slash, base+"/") {
		joined = filepath.Join(c.storageRoot, filepath.FromSlash(strings.TrimPrefix(slash, base+"/")))
		if _, err := os.Stat(joined); err == nil {
			return joined, nil
		}
	}
	// Как есть относительно CWD.
	if abs, err := filepath.Abs(p); err == nil {
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}
	return "", fmt.Errorf("resolve host path: not found: %s", hostPath)
}

// relUnderStorage — путь относительно storageRoot в slash-формате для Docker /data.
func (c *Clipper) relUnderStorage(absHost string) (string, error) {
	absHost = filepath.Clean(absHost)
	root := filepath.Clean(c.storageRoot)

	rel, err := filepath.Rel(root, absHost)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel), nil
	}

	// Сравнение без учёта регистра (Windows) через ToSlash lower.
	absSlash := strings.ToLower(filepath.ToSlash(absHost))
	rootSlash := strings.ToLower(filepath.ToSlash(root))
	if !strings.HasSuffix(rootSlash, "/") {
		rootSlash += "/"
	}
	if strings.HasPrefix(absSlash, rootSlash) {
		return filepath.ToSlash(absHost)[len(filepath.ToSlash(root))+1:], nil
	}
	return "", fmt.Errorf("path %s is outside storage root %s", absHost, root)
}

func parseFFmpegProbe(out []byte) (start, duration time.Duration, ok bool) {
	s := string(out)
	if m := reStart.FindStringSubmatch(s); len(m) == 2 {
		if sec, err := strconv.ParseFloat(m[1], 64); err == nil {
			start = time.Duration(sec * float64(time.Second))
		}
	}
	m := reDuration.FindStringSubmatch(s)
	if len(m) != 4 {
		return start, 0, false
	}
	h, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	sec, err := strconv.ParseFloat(m[3], 64)
	if err != nil {
		return start, 0, false
	}
	duration = time.Duration(h)*time.Hour + time.Duration(min)*time.Minute + time.Duration(sec*float64(time.Second))
	if duration <= 0 {
		return start, 0, false
	}
	return start, duration, true
}

// Snapshot захватывает кадр из живого RTSP (быстрое уведомление).
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
			"-frames:v", "1", "-update", "1", "-q:v", "2",
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
		"-y", "-rtsp_transport", "tcp",
		"-i", url,
		"-frames:v", "1", "-update", "1", "-q:v", "2",
		"/data/" + rel,
	}
	if o, err := c.runDocker(ctx, "", args...); err != nil {
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
