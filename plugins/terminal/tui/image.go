package tui

import (
	"encoding/base64"
	"os"
	"strconv"
	"strings"
)

// previewEscapes builds the concatenated inline-image escapes for the image
// attachments in atts, each capped at previewRows tall and stacked on their own
// lines, in whatever encoding caps says this terminal can draw. Unreadable,
// undecodable or oversized files are silently skipped — a preview is a nicety,
// never a reason to lose the chip or the turn.
func previewEscapes(atts []Attachment, caps Capabilities) string {
	var previews []string
	for _, a := range atts {
		if esc := previewEscape(a, caps); esc != "" {
			previews = append(previews, esc)
		}
	}
	return strings.Join(previews, "\n")
}

// previewEscape renders one attachment, or "" for anything that is not a
// drawable image. Every format the decoder knows arrives here, so JPEG, GIF and
// WebP are previewed too rather than dropped for not being PNG.
//
// Bytes that claim to be PNG and will not decode are handed to a kitty terminal
// untouched: the terminal is the final judge of its own format, and a decoder
// disagreement is a poor reason to withhold a preview it might well have drawn.
func previewEscape(a Attachment, caps Capabilities) string {
	if !strings.HasPrefix(a.Mime, "image/") {
		return ""
	}
	data, err := os.ReadFile(a.Path)
	if err != nil || len(data) == 0 {
		return ""
	}
	img, err := decodeImage(data)
	if err != nil {
		if caps.Graphics == GraphicsKitty && a.Mime == "image/png" && len(data) <= maxPreviewBytes {
			return kittyPreview(data, previewRows)
		}
		return ""
	}
	// The cap is on the escape now, not on the source file: what the viewport
	// re-scans on every repaint is the escape, and a photo that shrinks to a few
	// kilobytes used to be rejected for the size it arrived at.
	if esc := imageEscape(img, caps); len(esc) <= maxPreviewBytes {
		return esc
	}
	return ""
}

// previewRows caps the inline image preview height so a tall image cannot push
// the transcript off-screen (spec: bounded preview height).
const previewRows = 10

// maxPreviewBytes bounds the *encoded* payload of an inline preview, after
// downscaling — it used to bound the source file, which rejected a large photo
// that shrinks to a few kilobytes. What it protects is the screen: the kitty escape
// lives on its transcript line for the session, and the viewport re-scans every
// line's width (ansi.StringWidth) on each repaint — once per streamed chunk and
// per spinner frame while the tab is busy. A multi-MB base64 blob would make that
// scan dominate the frame, so larger images fall back to the chip alone. Well
// under maxAttachmentBytes (10 MiB): the attachment still reaches the agent full
// size; only the local thumbnail is skipped.
const maxPreviewBytes = 512 << 10

// kittyChunkBytes is the max base64 payload per kitty APC escape. The protocol
// requires transmission in chunks no larger than 4096 base64 bytes; each chunk
// after the first carries only the m (more) key.
const kittyChunkBytes = 4096

// kittyPreview encodes a PNG image as a kitty graphics-protocol escape that
// transmits and displays it inline, scaled to at most rows terminal rows (width
// inferred from the image's aspect ratio). It returns "" for an empty image so a
// caller can unconditionally append the result. Only the local terminal (a kitty
// runtime) interprets the escape; elsewhere it is inert and the chip stands alone.
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
			// a=T transmit-and-display, f=100 PNG, r caps the height in cells.
			b.WriteString("a=T,f=100,r=")
			b.WriteString(strconv.Itoa(rows))
			b.WriteString(",m=")
		} else {
			// Continuation chunks carry only the m (more) key.
			b.WriteString("m=")
		}
		b.WriteString(strconv.Itoa(more))
		b.WriteByte(';')
		b.WriteString(b64[i:end])
		b.WriteString("\x1b\\")
	}
	return b.String()
}
