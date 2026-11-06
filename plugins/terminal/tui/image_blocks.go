package tui

import (
	"image"
	"log/slog"
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
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return ""
	}
	rows := max(1, (bounds.Dy()*cols/bounds.Dx()+1)/pixelRowsPerLine)
	scaled := image.NewRGBA(image.Rect(0, 0, cols, rows*pixelRowsPerLine))
	xdraw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), img, bounds, xdraw.Src, nil)

	var builder strings.Builder
	for row := range rows {
		if row > 0 {
			builder.WriteByte('\n')
		}
		for col := range cols {
			builder.WriteString(sgr(sgrForeground, scaled.RGBAAt(col, row*pixelRowsPerLine)))
			builder.WriteString(sgr(sgrBackground, scaled.RGBAAt(col, row*pixelRowsPerLine+1)))
			builder.WriteString(blockUpperHalf)
		}
		builder.WriteString(ansiReset)
	}
	return builder.String()
}

func sgr(role int, c interface{ RGBA() (r, g, b, a uint32) }) string {
	red, green, blue, _ := c.RGBA()
	return "\x1b[" + strconv.Itoa(role) + ";2;" +
		strconv.Itoa(int(red>>byteShift)) + ";" + strconv.Itoa(int(green>>byteShift)) + ";" + strconv.Itoa(int(blue>>byteShift)) + "m"
}

const maxBlockCols = 200

func blockCols(img image.Image, rows int) int {
	bounds := img.Bounds()
	if bounds.Dy() <= 0 {
		return 1
	}
	return min(maxBlockCols, max(1, bounds.Dx()*rows*2/bounds.Dy()))
}

func imageEscape(log *slog.Logger, img image.Image, caps Capabilities) string {
	scaled := downscale(img)
	switch caps.Graphics {
	case GraphicsKitty:
		data, err := encodePNG(log, scaled)
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
