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

var kittyCaps = Capabilities{Graphics: GraphicsKitty}

func noiseImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rnd := rand.New(rand.NewSource(1))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(rnd.Intn(256)), G: uint8(rnd.Intn(256)), B: uint8(rnd.Intn(256)), A: 0xff})
		}
	}
	return img
}

func pngFile(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, noiseImage(w, h)); err != nil {
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
		{Name: "notes.txt", Path: txt, Mime: "text/plain"},
		{Name: "gone.png", Path: filepath.Join(dir, "gone.png"), Mime: "image/png"},
		{Name: "broken.png", Path: broken, Mime: "image/png"},
	}

	out := previewEscapes(atts, kittyCaps)
	if strings.Count(out, "\x1b_G") == 0 {
		t.Fatal("a readable PNG must produce a kitty graphics escape")
	}
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

func TestPreviewEscapesSkipsLargeImages(t *testing.T) {
	dir := t.TempDir()
	big := pngFile(t, dir, "big.png", 4000, 200)
	atts := []Attachment{{Name: "big.png", Path: big, Mime: "image/png"}}
	if out := previewEscapes(atts, kittyCaps); out != "" {
		t.Fatalf("an oversized preview must not be inlined, got %d bytes of escape", len(out))
	}
}
