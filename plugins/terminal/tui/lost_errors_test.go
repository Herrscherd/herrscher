package tui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureWarnings() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func expectWarning(t *testing.T, logs *bytes.Buffer, cause string) {
	t.Helper()
	line := logs.String()
	if !strings.Contains(line, "level=WARN") || !strings.Contains(line, cause) {
		t.Fatalf("the lost error must leave a warning carrying its cause %q, got %q", cause, line)
	}
}

func TestTranscriptImageFetchFailureLeavesAWarning(t *testing.T) {
	log, logs := captureWarnings()
	m := newTestModel()
	m.log = log
	m.caps = Capabilities{Graphics: GraphicsKitty}
	m.imageHosts = []string{"cdn.example.com"}
	m.imageFetcher = func(context.Context, string) ([]byte, error) { return nil, errors.New("connection reset by peer") }

	tb := m.ensureTab("a")
	tb.appendEntry(entry{role: roleAgent, text: "here: https://cdn.example.com/a.png"})
	if msg := runCmd(m.fetchEntryImages(tb, 0)); msg != nil {
		t.Fatalf("a failed fetch must produce no message, got %T", msg)
	}
	expectWarning(t, logs, "connection reset by peer")
}

func TestTranscriptImageDecodeFailureLeavesAWarning(t *testing.T) {
	log, logs := captureWarnings()
	m := newTestModel()
	m.log = log
	m.caps = Capabilities{Graphics: GraphicsKitty}
	m.imageHosts = []string{"cdn.example.com"}
	m.imageFetcher = func(context.Context, string) ([]byte, error) { return []byte("not an image"), nil }

	tb := m.ensureTab("a")
	tb.appendEntry(entry{role: roleAgent, text: "here: https://cdn.example.com/a.png"})
	if msg := runCmd(m.fetchEntryImages(tb, 0)); msg != nil {
		t.Fatalf("an undecodable image must produce no message, got %T", msg)
	}
	expectWarning(t, logs, "unknown format")
}

func TestAttachmentPreviewFailuresLeaveAWarning(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.png")
	garbage := filepath.Join(dir, "garbage.png")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(garbage, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		path  string
		cause string
	}{
		"missing file":     {filepath.Join(dir, "absent.png"), "no such file"},
		"empty file":       {empty, "image.empty"},
		"undecodable file": {garbage, "unknown format"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			log, logs := captureWarnings()
			if esc := previewEscape(log, Attachment{Mime: "image/png", Path: tc.path}, Capabilities{Graphics: GraphicsKitty}); esc != "" {
				t.Fatalf("a failed preview must draw nothing, got %q", esc)
			}
			expectWarning(t, logs, tc.cause)
		})
	}
}

func TestStatuslineFailureLeavesOneWarningPerFailure(t *testing.T) {
	log, logs := captureWarnings()
	m := &model{log: log}
	m.receiveScript(scriptDoneMsg{err: errStatuslineNoCommand})
	m.receiveScript(scriptDoneMsg{err: errStatuslineNoCommand})
	expectWarning(t, logs, "statusline.no_command")
	if n := strings.Count(logs.String(), "statusline script failed"); n != 1 {
		t.Fatalf("a run of identical failures must leave one warning, got %d: %q", n, logs.String())
	}
}

type oversizedImage struct{}

func (oversizedImage) ColorModel() color.Model { return color.RGBAModel }

func (oversizedImage) Bounds() image.Rectangle { return image.Rect(0, 0, 1<<32, 1) }

func (oversizedImage) At(int, int) color.Color { return color.Black }

func TestPNGEncodingFailureLeavesAWarning(t *testing.T) {
	log, logs := captureWarnings()
	if _, err := encodePNG(log, oversizedImage{}); err == nil {
		t.Fatal("an image beyond the PNG size limit must fail to encode")
	}
	expectWarning(t, logs, "invalid image size")
}
