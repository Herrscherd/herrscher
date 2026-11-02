package tui

import (
	"encoding/base64"
	"image"
	"os"
	"strconv"
	"strings"
)

func previewEscapes(atts []Attachment, caps Capabilities) string {
	var previews []string
	for _, a := range atts {
		if esc := previewEscape(a, caps); esc != "" {
			previews = append(previews, esc)
		}
	}
	return strings.Join(previews, "\n")
}

func previewEscape(a Attachment, caps Capabilities) string {
	if !strings.HasPrefix(a.Mime, "image/") {
		return ""
	}
	if st, err := os.Stat(a.Path); err != nil || st.Size() == 0 || st.Size() > maxDecodeBytes {
		return ""
	}
	data, err := os.ReadFile(a.Path)
	if err != nil || len(data) == 0 {
		return ""
	}
	img, err := decodeImage(data)
	if err != nil {
		return ""
	}
	return boundedEscape(img, caps)
}

func boundedEscape(img image.Image, caps Capabilities) string {
	if esc := imageEscape(img, caps); len(esc) <= maxPreviewBytes {
		return esc
	}
	return ""
}

const previewRows = 10

const maxPreviewBytes = 512 << 10

const kittyChunkBytes = 4096

func kittyPreview(png []byte, rows int) string {
	if len(png) == 0 {
		return ""
	}
	b64 := base64.StdEncoding.EncodeToString(png)

	var b strings.Builder
	for i := 0; i < len(b64); i += kittyChunkBytes {
		end := i + kittyChunkBytes
		if end > len(b64) {
			end = len(b64)
		}
		more := 0
		if end < len(b64) {
			more = 1
		}
		b.WriteString("\x1b_G")
		if i == 0 {
			b.WriteString("a=T,f=100,r=")
			b.WriteString(strconv.Itoa(rows))
			b.WriteString(",m=")
		} else {
			b.WriteString("m=")
		}
		b.WriteString(strconv.Itoa(more))
		b.WriteByte(';')
		b.WriteString(b64[i:end])
		b.WriteString("\x1b\\")
	}
	return b.String()
}
