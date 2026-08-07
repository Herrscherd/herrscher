package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kittyCaps is the terminal these preview tests are written against: the kitty
// protocol is the one whose payload round-trips to a decodable image.
var kittyCaps = Capabilities{Graphics: GraphicsKitty}

// pngFile writes a w×h PNG and returns its path. Real bytes rather than a
// plausible-looking blob: the preview path decodes what it is given, so a fixture
// that does not decode would test the skip and call it a preview.
func pngFile(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Noise, not a gradient: PNG would compress a smooth image to a few hundred
	// bytes whatever its dimensions, and the size bound under test is on bytes.
	rnd := rand.New(rand.NewSource(1))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(rnd.Intn(256)), G: uint8(rnd.Intn(256)), B: uint8(rnd.Intn(256)), A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPreviewEscapesOnlyReadableImages(t *testing.T) {
	dir := t.TempDir()
	shot := pngFile(t, dir, "shot.png", 8, 8)
	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(broken, bytes.Repeat([]byte("PNG."), 100), 0o644); err != nil {
		t.Fatal(err)
	}

	atts := []Attachment{
		{Name: "shot.png", Path: shot, Mime: "image/png"},
		{Name: "notes.txt", Path: txt, Mime: "text/plain"},                          // not an image → skipped
		{Name: "gone.png", Path: filepath.Join(dir, "gone.png"), Mime: "image/png"}, // missing → skipped
		{Name: "broken.png", Path: broken, Mime: "image/png"},                       // undecodable → skipped
	}

	out := previewEscapes(atts, kittyCaps)
	if strings.Count(out, "\x1b_G") == 0 {
		t.Fatal("a readable PNG must produce a kitty graphics escape")
	}
	// Exactly one image contributed, and what it carries is a decodable PNG: the
	// bytes the terminal is handed are bytes this package has read.
	if _, err := decodeImage(apcPayload(t, out)); err != nil {
		t.Fatalf("the transmitted payload must be a decodable image: %v", err)
	}
	if strings.Contains(out, "\n") {
		t.Fatalf("only the one readable image may be previewed: %q", out[:min(len(out), 80)])
	}
}

func TestPreviewEscapesEmptyWhenNoImages(t *testing.T) {
	if out := previewEscapes([]Attachment{{Name: "a.txt", Mime: "text/plain"}}, kittyCaps); out != "" {
		t.Fatalf("no images must yield no escape, got %q", out)
	}
}

// TestPreviewEscapesSkipsLargeImages guards the inline-preview budget: an image
// whose *escape* outgrows maxPreviewBytes must fall back to the chip alone, so
// its base64 blob is never re-scanned by the viewport's per-repaint width
// computation. The bound is on the escape, so the fixture is an image that stays
// large after downscaling — wide rather than tall, since only height is capped.
func TestPreviewEscapesSkipsLargeImages(t *testing.T) {
	dir := t.TempDir()
	big := pngFile(t, dir, "big.png", 4000, 200)
	atts := []Attachment{{Name: "big.png", Path: big, Mime: "image/png"}}
	if out := previewEscapes(atts, kittyCaps); out != "" {
		t.Fatalf("an oversized preview must not be inlined, got %d bytes of escape", len(out))
	}
}
