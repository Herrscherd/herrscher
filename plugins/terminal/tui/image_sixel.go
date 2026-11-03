package tui

import (
	"image"
	"sort"
	"strconv"
	"strings"
)

const sixelCubeSide = 6

func sixelEscape(img image.Image) string {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return ""
	}
	idx, colours := quantiseSixel(img, width, height)
	var builder strings.Builder
	writeSixelHeader(&builder, width, height, colours)
	writeSixelBands(&builder, idx, width, height, colours)
	builder.WriteString("\x1b\\")
	return builder.String()
}

func quantiseSixel(img image.Image, width, height int) (indices, palette []int) {
	bounds := img.Bounds()
	idx := make([]int, width*height)
	present := make(map[int]bool)
	for row := range height {
		for col := range width {
			r, g, bl, _ := img.At(bounds.Min.X+col, bounds.Min.Y+row).RGBA()
			index := sixelLevel(r)*sixelCubeSide*sixelCubeSide + sixelLevel(g)*sixelCubeSide + sixelLevel(bl)
			idx[row*width+col] = index
			present[index] = true
		}
	}
	colours := make([]int, 0, len(present))
	for i := range present {
		colours = append(colours, i)
	}
	sort.Ints(colours)
	return idx, colours
}

func writeSixelHeader(builder *strings.Builder, width, height int, colours []int) {
	builder.WriteString("\x1bPq")
	builder.WriteString(`"1;1;`)
	builder.WriteString(strconv.Itoa(width))
	builder.WriteByte(';')
	builder.WriteString(strconv.Itoa(height))
	for _, index := range colours {
		builder.WriteByte('#')
		builder.WriteString(strconv.Itoa(index))
		builder.WriteString(";2;")
		row := index / (sixelCubeSide * sixelCubeSide)
		glyph := (index / sixelCubeSide) % sixelCubeSide
		bl := index % sixelCubeSide
		builder.WriteString(sixelPercent(row) + ";" + sixelPercent(glyph) + ";" + sixelPercent(bl))
	}
}

func writeSixelBands(builder *strings.Builder, idx []int, width, height int, colours []int) {
	for band := 0; band < height; band += 6 {
		first := true
		for _, colour := range colours {
			row := sixelBand(idx, width, height, band, colour)
			if row == "" {
				continue
			}
			if !first {
				builder.WriteByte('$')
			}
			first = false
			builder.WriteByte('#')
			builder.WriteString(strconv.Itoa(colour))
			builder.WriteString(row)
		}
		if band+6 < height {
			builder.WriteByte('-')
		}
	}
}

func sixelBand(idx []int, width, height, band, colour int) string {
	var strip strings.Builder
	inked := false
	for col := range width {
		var bits byte
		for row := range 6 {
			y := band + row
			if y < height && idx[y*width+col] == colour {
				bits |= 1 << row
			}
		}
		if bits != 0 {
			inked = true
		}
		strip.WriteByte(sixelBase + bits)
	}
	if !inked {
		return ""
	}
	return strip.String()
}

const (
	sixelBase     = 0x3f
	channelLevels = 256
)

func sixelLevel(v uint32) int {
	level := int(v>>byteShift) * sixelCubeSide / channelLevels
	if level >= sixelCubeSide {
		level = sixelCubeSide - 1
	}
	return level
}

func sixelPercent(level int) string {
	return strconv.Itoa(level * 100 / (sixelCubeSide - 1))
}
