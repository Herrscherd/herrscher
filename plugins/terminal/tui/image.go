package tui

import (
	"encoding/base64"
	"fmt"
	"image"
	"log/slog"
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

func previewEscape(attachment Attachment, caps Capabilities) string {
	if !strings.HasPrefix(attachment.Mime, "image/") {
		return ""
	}
	info, err := os.Stat(attachment.Path)
	if err != nil {
		return skipPreview(err)
	}
	if info.Size() == 0 {
		return skipPreview(errEmptyImage)
	}
	if info.Size() > maxDecodeBytes {
		return skipPreview(fmt.Errorf("%w: %d/%d", errImageTooLarge, info.Size(), maxDecodeBytes))
	}
	data, err := os.ReadFile(attachment.Path)
	if err != nil {
		return skipPreview(err)
	}
	if len(data) == 0 {
		return skipPreview(errEmptyImage)
	}
	img, err := decodeImage(data)
	if err != nil {
		return skipPreview(err)
	}
	return boundedEscape(img, caps)
}

func skipPreview(err error) string {
	slog.Warn("attachment preview skipped", "err", err)
	return ""
}

func boundedEscape(img image.Image, caps Capabilities) string {
	esc := imageEscape(img, caps)
	if len(esc) <= maxPreviewBytes {
		return esc
	}
	slog.Warn("image preview dropped, its escape sequence is over the cap", "bytes", len(esc), "max", maxPreviewBytes)
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

	var builder strings.Builder
	for index := 0; index < len(b64); index += kittyChunkBytes {
		end := index + kittyChunkBytes
		if end > len(b64) {
			end = len(b64)
		}
		more := 0
		if end < len(b64) {
			more = 1
		}
		builder.WriteString("\x1b_G")
		if index == 0 {
			builder.WriteString("a=T,f=100,r=")
			builder.WriteString(strconv.Itoa(rows))
			builder.WriteString(",m=")
		} else {
			builder.WriteString("m=")
		}
		builder.WriteString(strconv.Itoa(more))
		builder.WriteByte(';')
		builder.WriteString(b64[index:end])
		builder.WriteString("\x1b\\")
	}
	return builder.String()
}
