package tui

import (
	"encoding/base64"
	"errors"
	"os"
	"strconv"
	"strings"
)

// previewEscapes builds the concatenated kitty graphics escapes for the image
// attachments in atts, each capped at previewRows tall and stacked on their own
// lines. Unreadable, undecodable or oversized files are silently skipped — a
// preview is a nicety, never a reason to lose the chip or the turn. Callers gate
// this on the probe's graphics capability; the escapes are inert elsewhere.
func previewEscapes(atts []Attachment) string {
	var previews []string
	for _, a := range atts {
		data, err := previewPayload(a)
		// The cap is on the *encoded* payload now: what the viewport re-scans on
		// every repaint is the escape, not the file it came from.
		if err != nil || len(data) == 0 || len(data) > maxPreviewBytes {
			continue
		}
		previews = append(previews, kittyPreview(data, previewRows))
	}
	return strings.Join(previews, "\n")
}

// previewPayload reads an image attachment and returns the PNG bytes kitty
// transmits (f=100), downscaled to previewRows. Every format the decoder knows
// arrives as PNG, so JPEG, GIF and WebP are previewed too rather than dropped.
//
// Bytes that claim to be PNG and will not decode are handed over untouched: the
// terminal is the final judge of its own format, and a decoder disagreement is a
// poor reason to withhold a preview the terminal might well have drawn.
func previewPayload(a Attachment) ([]byte, error) {
	if !strings.HasPrefix(a.Mime, "image/") {
		return nil, errNotAnImage
	}
	data, err := os.ReadFile(a.Path)
	if err != nil {
		return nil, err
	}
	img, err := decodeImage(data)
	if err != nil {
		if a.Mime == "image/png" {
			return data, nil
		}
		return nil, err
	}
	return encodePNG(downscale(img, previewRows))
}

var errNotAnImage = errors.New("attachment is not an image")

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
