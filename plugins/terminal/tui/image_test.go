package tui

import (
	"bytes"
	"encoding/base64"
	"image"
	"math/rand/v2"
	"strings"
	"testing"
)

func apcPayload(t *testing.T, s string) []byte {
	t.Helper()
	var b64 strings.Builder
	for _, block := range strings.Split(s, "\x1b\\") {
		i := strings.Index(block, "\x1b_G")
		if i < 0 {
			continue
		}
		semi := strings.Index(block, ";")
		if semi < 0 {
			t.Fatalf("APC block has no ';' separator: %q", block)
		}
		b64.WriteString(block[semi+1:])
	}
	raw, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil {
		t.Fatalf("payload is not valid base64: %v", err)
	}
	return raw
}

func TestKittyPreviewRoundTripsChunkedPayload(t *testing.T) {
	png := bytes.Repeat([]byte("PNGDATA."), 700)
	out := kittyPreview(png, 3)

	if !strings.HasPrefix(out, "\x1b_G") {
		t.Fatalf("preview must start with the kitty APC intro, got %q", out[:min(8, len(out))])
	}
	if !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("preview must end with the APC terminator ESC-backslash")
	}
	first, _, _ := strings.Cut(out, ";")
	for _, want := range []string{"a=T", "f=100", "r=3"} {
		if !strings.Contains(first, want) {
			t.Errorf("first APC block %q missing control key %q", first, want)
		}
	}
	if got := apcPayload(t, out); !bytes.Equal(got, png) {
		t.Fatalf("round-tripped payload (%d bytes) != original (%d bytes)", len(got), len(png))
	}
}

func TestKittyPreviewEmptyIsNoOp(t *testing.T) {
	if out := kittyPreview(nil, 3); out != "" {
		t.Fatalf("empty image must yield no escape, got %q", out)
	}
}

func TestBoundedEscapeWarnsWithItsSizeWhenItDropsAnOverlongPreview(t *testing.T) {
	logs := captureWarnings(t)
	noise := image.NewNRGBA(image.Rect(0, 0, maxPreviewWidth, previewRows*cellPixels))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range noise.Pix {
		noise.Pix[i] = uint8(rng.UintN(256))
	}
	if esc := boundedEscape(noise, Capabilities{Graphics: GraphicsKitty}); esc != "" {
		t.Fatalf("an over-long preview must be dropped, got %d bytes", len(esc))
	}
	expectWarning(t, logs, "image preview dropped")
	if !strings.Contains(logs.String(), "bytes=") || !strings.Contains(logs.String(), "max=") {
		t.Fatalf("the drop warning must carry the size and the cap: %q", logs.String())
	}
}

func TestBoundedEscapeKeepsASmallPreviewSilently(t *testing.T) {
	logs := captureWarnings(t)
	small := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	if esc := boundedEscape(small, Capabilities{Graphics: GraphicsKitty}); esc == "" {
		t.Fatal("a small preview must be kept")
	}
	if logs.Len() != 0 {
		t.Fatalf("a kept preview must not warn: %q", logs.String())
	}
}
