package tui

import (
	"image"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
)

const (
	pixelRowsPerLine = 2
	sgrForeground    = 38
	sgrBackground    = 48
	byteShift        = 8
)

const blockUpperHalf = "▀"

const ansiReset = "\x1b[0m"

func blockRender(img image.Image, cols int) string {
	if cols < 1 {
		cols = 1
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return ""
	}
	rows := max(1, (b.Dy()*cols/b.Dx()+1)/pixelRowsPerLine)
	scaled := image.NewRGBA(image.Rect(0, 0, cols, rows*pixelRowsPerLine))
	xdraw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), img, b, xdraw.Src, nil)

	var s strings.Builder
	for row := range rows {
		if row > 0 {
			s.WriteByte('\n')
		}
		for x := range cols {
			s.WriteString(sgr(sgrForeground, scaled.RGBAAt(x, row*pixelRowsPerLine)))
			s.WriteString(sgr(sgrBackground, scaled.RGBAAt(x, row*pixelRowsPerLine+1)))
			s.WriteString(blockUpperHalf)
		}
		s.WriteString(ansiReset)
	}
	return s.String()
}

func sgr(role int, c interface{ RGBA() (r, g, b, a uint32) }) string {
	r, g, b, _ := c.RGBA()
	return "\x1b[" + strconv.Itoa(role) + ";2;" +
		strconv.Itoa(int(r>>byteShift)) + ";" + strconv.Itoa(int(g>>byteShift)) + ";" + strconv.Itoa(int(b>>byteShift)) + "m"
}

const maxBlockCols = 200

func blockCols(img image.Image, rows int) int {
	b := img.Bounds()
	if b.Dy() <= 0 {
		return 1
	}
	return min(maxBlockCols, max(1, b.Dx()*rows*2/b.Dy()))
}

func imageEscape(img image.Image, caps Capabilities) string {
	scaled := downscale(img)
	switch caps.Graphics {
	case GraphicsKitty:
		data, err := encodePNG(scaled)
		if err != nil {
			break
		}
		return kittyPreview(data, previewRows)
	case GraphicsSixel:
		return sixelEscape(scaled)
	case GraphicsNone:
	}
	return blockRender(scaled, blockCols(scaled, previewRows))
}
