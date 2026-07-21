package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewEscapesOnlyReadablePNGs(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "shot.png")
	pngBytes := bytes.Repeat([]byte("PNG."), 100)
	if err := os.WriteFile(png, pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	atts := []Attachment{
		{Name: "shot.png", Path: png, Mime: "image/png", Size: int64(len(pngBytes))},
		{Name: "notes.txt", Path: txt, Mime: "text/plain", Size: 2},                 // not an image → skipped
		{Name: "gone.png", Path: filepath.Join(dir, "gone.png"), Mime: "image/png"}, // missing → skipped
	}

	out := previewEscapes(atts)
	if out == "" {
		t.Fatal("a readable PNG must produce a preview escape")
	}
	if strings.Count(out, "\x1b_G") == 0 {
		t.Fatal("output must contain a kitty graphics escape")
	}
	// Exactly one image contributed: its payload round-trips to pngBytes.
	if got := apcPayload(t, out); !bytes.Equal(got, pngBytes) {
		t.Fatalf("payload (%d bytes) != the single PNG (%d bytes)", len(got), len(pngBytes))
	}
}

func TestPreviewEscapesEmptyWhenNoImages(t *testing.T) {
	if out := previewEscapes([]Attachment{{Name: "a.txt", Mime: "text/plain"}}); out != "" {
		t.Fatalf("no images must yield no escape, got %q", out)
	}
}

// TestPreviewEscapesSkipsLargeImages guards the inline-preview budget: an image
// that is small enough to stage (≤ maxAttachmentBytes) but larger than
// maxPreviewBytes must fall back to the chip alone, so its base64 blob is never
// re-scanned by the viewport's per-repaint width computation.
func TestPreviewEscapesSkipsLargeImages(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), maxPreviewBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	atts := []Attachment{{Name: "big.png", Path: big, Mime: "image/png", Size: maxPreviewBytes + 1}}
	if out := previewEscapes(atts); out != "" {
		t.Fatalf("an oversized image must not be previewed inline, got %d bytes of escape", len(out))
	}
}
