package clipper

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveHostRelativeStoragePrefix(t *testing.T) {
	dir := t.TempDir()
	recDir := filepath.Join(dir, "recordings", "cam_x")
	if err := os.MkdirAll(recDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(recDir, "seg.mp4")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(dir)
	// Как в БД после scan из CWD: "storage/recordings/..." где storage = basename
	rel := filepath.ToSlash(filepath.Join(filepath.Base(dir), "recordings", "cam_x", "seg.mp4"))
	got, err := c.resolveHost(rel)
	if err != nil {
		// также допускаем путь без префикса basename
		got, err = c.resolveHost("recordings/cam_x/seg.mp4")
	}
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("resolved missing: %s", got)
	}
}

func TestContentDurationPrefersVideo(t *testing.T) {
	info := ProbeInfo{
		FormatDuration: 341760 * time.Millisecond,
		VideoDuration:  300505 * time.Millisecond,
	}
	if info.ContentDuration() != info.VideoDuration {
		t.Fatalf("got %v", info.ContentDuration())
	}

	nameTime := time.Date(2026, 9, 12, 12, 19, 31, 0, time.UTC)
	nextName := time.Date(2026, 9, 12, 12, 25, 12, 0, time.UTC)
	contentStart := nextName.Add(-info.ContentDuration())
	lead := contentStart.Sub(nameTime)
	if lead < 35*time.Second || lead > 50*time.Second {
		t.Fatalf("expected ~41s audio lead before video, got %v (start=%v)", lead, contentStart)
	}

	wrong := nextName.Add(-info.FormatDuration)
	if wrong.Sub(nameTime) > 2*time.Second {
		t.Fatalf("sanity: format-based start should be near name, got delta %v", wrong.Sub(nameTime))
	}
}

func TestParseFFmpegProbe(t *testing.T) {
	out := []byte(`Input #0, mov,mp4,mp4a:
  Duration: 00:04:57.12, start: 1.480000, bitrate: 2000 kb/s
`)
	start, dur, ok := parseFFmpegProbe(out)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if start != 1480*time.Millisecond {
		t.Fatalf("start=%v", start)
	}
	want := 4*time.Minute + 57*time.Second + 120*time.Millisecond
	if dur != want {
		t.Fatalf("duration=%v want %v", dur, want)
	}
}

func TestProbeRejectsMissing(t *testing.T) {
	c := New(t.TempDir())
	_, err := c.Probe(context.Background(), "recordings/missing.mp4")
	if err == nil {
		t.Fatal("expected error")
	}
}
