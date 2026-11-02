package tui

import (
	"image"
	"sort"
	"strconv"
	"strings"
)

const sixelCubeSide = 6

func sixelEscape(img image.Image) string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return ""
	}

	idx := make([]int, w*h)
	present := map[int]bool{}
	for y := range h {
		for x := range w {
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
	sort.Ints(colours)

	var s strings.Builder
	s.WriteString("\x1bPq")
	s.WriteString(`"1;1;`)
	s.WriteString(strconv.Itoa(w))
	s.WriteByte(';')
	s.WriteString(strconv.Itoa(h))
	for _, i := range colours {
		s.WriteByte('#')
		s.WriteString(strconv.Itoa(i))
		s.WriteString(";2;")
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
				continue
			}
			if !first {
				s.WriteByte('$')
			}
			first = false
			s.WriteByte('#')
			s.WriteString(strconv.Itoa(colour))
			s.WriteString(row)
		}
		if band+6 < h {
			s.WriteByte('-')
		}
	}
	s.WriteString("\x1b\\")
	return s.String()
}

func sixelBand(idx []int, w, h, band, colour int) string {
	var row strings.Builder
	any := false
	for x := range w {
		var bits byte
		for r := range 6 {
			y := band + r
			if y < h && idx[y*w+x] == colour {
				bits |= 1 << r
			}
		}
		if bits != 0 {
			any = true
		}
		row.WriteByte(0x3f + bits)
	}
	if !any {
		return ""
	}
	return row.String()
}

func sixelLevel(v uint32) int {
	l := int(v>>8) * sixelCubeSide / 256
	if l >= sixelCubeSide {
		l = sixelCubeSide - 1
	}
	return l
}

func sixelPercent(level int) string {
	return strconv.Itoa(level * 100 / (sixelCubeSide - 1))
}
