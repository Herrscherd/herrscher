package tui

import (
	"image"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// The half-block encoder is the one that always works. It needs no protocol at
// all — a character and two SGR colours, which every terminal built in the last
// thirty years draws — so a terminal with no graphics support sees the picture
// coarsely instead of seeing a blank and being told nothing.

// blockUpperHalf fills the top half of its cell, so the foreground colour paints
// the upper pixel row and the background colour the lower one: two pixel rows per
// text row, which is what makes the aspect ratio come out roughly square.
const blockUpperHalf = "▀"

// ansiReset closes a row's colours. Without it the last cell's background bleeds
// across the rest of the transcript line.
const ansiReset = "\x1b[0m"

// blockRender draws img as half-block characters cols wide. Colours are written
// as raw truecolor SGR rather than through the palette: these are the image's own
// pixels, not a role in the theme, and lipgloss would drop them when it renders
// without a terminal attached.
func blockRender(img image.Image, cols int) string {
	if cols < 1 {
		cols = 1
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return ""
	}
	// Two pixel rows per text row, and a terminal cell is about twice as tall as
	// it is wide — so sampling one pixel per half-cell keeps the picture square.
	rows := max(1, (b.Dy()*cols/b.Dx()+1)/2)
	scaled := image.NewRGBA(image.Rect(0, 0, cols, rows*2))
	xdraw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), img, b, xdraw.Src, nil)

	var s strings.Builder
	for row := 0; row < rows; row++ {
		if row > 0 {
			s.WriteByte('\n')
		}
		for x := 0; x < cols; x++ {
			s.WriteString(sgr(38, scaled.RGBAAt(x, row*2)))
			s.WriteString(sgr(48, scaled.RGBAAt(x, row*2+1)))
			s.WriteString(blockUpperHalf)
		}
		s.WriteString(ansiReset)
	}
	return s.String()
}

// sgr writes one truecolor SGR: 38 sets the foreground, 48 the background.
func sgr(role int, c interface{ RGBA() (r, g, b, a uint32) }) string {
	r, g, b, _ := c.RGBA()
	return "\x1b[" + strconv.Itoa(role) + ";2;" +
		strconv.Itoa(int(r>>8)) + ";" + strconv.Itoa(int(g>>8)) + ";" + strconv.Itoa(int(b>>8)) + "m"
}

// maxBlockCols bounds the drawn width in cells. A bound on rows alone does not
// bound the picture: a 2000×10 banner is already short enough to keep every row
// and comes out four thousand cells wide, on one transcript line the viewport
// re-measures on every repaint. No terminal is this wide, and clamping the width
// shrinks the rows with it, so the picture keeps its shape.
const maxBlockCols = 200

// blockCols is how wide to draw img so it occupies at most rows text rows. It is
// the half-block counterpart of the r= key the kitty escape carries: the bound
// the transcript needs is on height, and width follows from the aspect ratio —
// up to maxBlockCols, past which the height stops being the binding constraint.
func blockCols(img image.Image, rows int) int {
	b := img.Bounds()
	if b.Dy() <= 0 {
		return 1
	}
	return min(maxBlockCols, max(1, b.Dx()*rows*2/b.Dy()))
}

// imageEscape renders img with the best encoder this terminal has: the kitty
// graphics protocol, then sixel, then half-blocks. It is the single place the
// choice is made, so adding a protocol is one case rather than a search of the
// package.
func imageEscape(img image.Image, caps Capabilities) string {
	scaled := downscale(img, previewRows)
	switch caps.Graphics {
	case GraphicsKitty:
		data, err := encodePNG(scaled)
		if err != nil {
			// Even the kitty path degrades rather than failing: half-blocks need no
			// encoder at all, so there is always something to draw.
			break
		}
		return kittyPreview(data, previewRows)
	case GraphicsSixel:
		return sixelEscape(scaled)
	}
	return blockRender(scaled, blockCols(scaled, previewRows))
}
