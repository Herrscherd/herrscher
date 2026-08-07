package tui

import (
	"image"
	"sort"
	"strconv"
	"strings"
)

// Sixel predates the kitty protocol by forty years and is the fallback a good
// many terminals do have: six pixel rows are packed into the low bits of one
// printable byte, one colour at a time, six rows to a band.

// sixelCubeSide is the side of the colour cube every pixel is quantised onto.
// Sixel re-states the palette on every image, and a 216-entry cube costs about
// two kilobytes of preamble — small against the pixel data, and uniform enough
// that a photograph does not band visibly at preview size.
const sixelCubeSide = 6

// sixelEscape encodes img as a sixel DCS sequence. It returns "" for an empty
// image so a caller can append the result unconditionally. Only a sixel terminal
// interprets the sequence; the caller picks the encoder from the probe.
func sixelEscape(img image.Image) string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return ""
	}

	idx := make([]int, w*h)
	present := map[int]bool{}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := sixelLevel(r)*sixelCubeSide*sixelCubeSide + sixelLevel(g)*sixelCubeSide + sixelLevel(bl)
			idx[y*w+x] = i
			present[i] = true
		}
	}
	colours := make([]int, 0, len(present))
	for i := range present {
		colours = append(colours, i)
	}
	sort.Ints(colours) // a stable palette order keeps the output diffable

	var s strings.Builder
	s.WriteString("\x1bPq")
	// Raster attributes: 1:1 pixel aspect, then the image's own size, so the
	// terminal reserves the right number of cells before any pixel arrives.
	s.WriteString(`"1;1;`)
	s.WriteString(strconv.Itoa(w))
	s.WriteByte(';')
	s.WriteString(strconv.Itoa(h))
	for _, i := range colours {
		s.WriteByte('#')
		s.WriteString(strconv.Itoa(i))
		s.WriteString(";2;")
		// Sixel states RGB in percent, not in bytes.
		r := i / (sixelCubeSide * sixelCubeSide)
		g := (i / sixelCubeSide) % sixelCubeSide
		bl := i % sixelCubeSide
		s.WriteString(sixelPercent(r) + ";" + sixelPercent(g) + ";" + sixelPercent(bl))
	}

	for band := 0; band < h; band += 6 {
		first := true
		for _, colour := range colours {
			row := sixelBand(idx, w, h, band, colour)
			if row == "" {
				continue // this colour does not appear in this band
			}
			if !first {
				s.WriteByte('$') // carriage return: overprint the band in the next colour
			}
			first = false
			s.WriteByte('#')
			s.WriteString(strconv.Itoa(colour))
			s.WriteString(row)
		}
		if band+6 < h {
			s.WriteByte('-') // next band
		}
	}
	s.WriteString("\x1b\\")
	return s.String()
}

// sixelBand renders one colour's contribution to one six-row band, or "" when
// the colour does not appear in it — skipping absent colours is what keeps the
// payload proportional to the picture rather than to the palette.
func sixelBand(idx []int, w, h, band, colour int) string {
	var row strings.Builder
	any := false
	for x := 0; x < w; x++ {
		var bits byte
		for r := 0; r < 6; r++ {
			y := band + r
			if y < h && idx[y*w+x] == colour {
				bits |= 1 << r
			}
		}
		if bits != 0 {
			any = true
		}
		row.WriteByte(0x3f + bits) // the sixel alphabet starts at '?'
	}
	if !any {
		return ""
	}
	return row.String()
}

// sixelLevel maps a 16-bit colour channel onto the cube's 0..5 levels.
func sixelLevel(v uint32) int {
	l := int(v>>8) * sixelCubeSide / 256
	if l >= sixelCubeSide {
		l = sixelCubeSide - 1
	}
	return l
}

// sixelPercent turns a cube level into the 0..100 the protocol wants.
func sixelPercent(level int) string {
	return strconv.Itoa(level * 100 / (sixelCubeSide - 1))
}
