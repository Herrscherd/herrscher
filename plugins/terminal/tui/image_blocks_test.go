package tui

import (
	"image"
	"strings"
	"testing"
)

func TestBlockRenderPacksTwoPixelRowsPerTextRow(t *testing.T) {
	out := blockRender(twoByTwo(), 2)
	if n := strings.Count(out, "\n"); n != 0 {
		t.Fatalf("a 2×2 image must render as one row, got %d line breaks: %q", n+1, out)
	}
	if got := strings.Count(out, blockUpperHalf); got != 2 {
		t.Fatalf("a two-column image must render two half-block cells, got %d: %q", got, out)
	}
	if !strings.Contains(out, "38;2;255;0;0") {
		t.Errorf("the top-left pixel must reach the foreground: %q", out)
	}
	if !strings.Contains(out, "48;2;0;0;255") {
		t.Errorf("the bottom-left pixel must reach the background: %q", out)
	}
	if !strings.HasSuffix(out, ansiReset) {
		t.Errorf("a row must reset the colour it set, or it bleeds into the transcript: %q", out)
	}
}

func TestBlockRenderKeepsAnOddLastRow(t *testing.T) {
	out := blockRender(image.NewRGBA(image.Rect(0, 0, 2, 3)), 2)
	if n := strings.Count(out, "\n"); n != 1 {
		t.Fatalf("three pixel rows must render as two text rows, got %d: %q", n+1, out)
	}
}

func TestBlockRenderEmptyIsNoOp(t *testing.T) {
	if out := blockRender(image.NewRGBA(image.Rect(0, 0, 0, 0)), 10); out != "" {
		t.Fatalf("an empty image must render nothing, got %q", out)
	}
}

func TestImageEscapePicksTheEncoderFromTheProbe(t *testing.T) {
	img := twoByTwo()
	kitty := imageEscape(img, Capabilities{Graphics: GraphicsKitty})
	if !strings.HasPrefix(kitty, "\x1b_G") {
		t.Errorf("kitty must take the kitty graphics protocol: %q", kitty)
	}
	sixel := imageEscape(img, Capabilities{Graphics: GraphicsSixel})
	if !strings.HasPrefix(sixel, "\x1bPq") {
		t.Errorf("a sixel terminal must take the sixel encoder: %q", sixel)
	}
	blocks := imageEscape(img, Capabilities{Graphics: GraphicsNone})
	if !strings.Contains(blocks, blockUpperHalf) {
		t.Errorf("a terminal with no protocol must fall back to half-blocks: %q", blocks)
	}
}
