package tui

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// twoByTwo is the fixture both new encoders are read against: the top row red
// then green, the bottom row blue then white, so a mistake in row or column
// order shows up as a different colour rather than as a subtler shift.
func twoByTwo() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	img.Set(1, 0, color.RGBA{G: 0xff, A: 0xff})
	img.Set(0, 1, color.RGBA{B: 0xff, A: 0xff})
	img.Set(1, 1, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	return img
}

func TestSixelEscapeShape(t *testing.T) {
	out := sixelEscape(twoByTwo())
	if !strings.HasPrefix(out, "\x1bPq") {
		t.Fatalf("sixel must open with the DCS introducer ESC P q: %q", out[:min(8, len(out))])
	}
	if !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("sixel must close with the string terminator ESC-backslash: %q", out)
	}
	if !strings.Contains(out, `"1;1;2;2`) {
		t.Errorf("sixel must declare its raster size: %q", out)
	}
	// One colour definition per distinct colour in the fixture, each in the
	// #index;2;r;g;b form the protocol defines for RGB.
	if got := strings.Count(out, ";2;"); got < 4 {
		t.Errorf("four distinct colours must each be defined, got %d definitions: %q", got, out)
	}
	// A two-row image fits one sixel band, so there is no band separator.
	if strings.Contains(out, "-") {
		t.Errorf("a two-row image needs no band separator: %q", out)
	}
}

func TestSixelEscapeEmptyIsNoOp(t *testing.T) {
	if out := sixelEscape(image.NewRGBA(image.Rect(0, 0, 0, 0))); out != "" {
		t.Fatalf("an empty image must yield no escape, got %q", out)
	}
}
