package tui

import (
	"encoding/base64"
	"os"
	"strconv"
	"strings"
)

// previewEscapes builds the concatenated kitty graphics escapes for the PNG
// attachments in atts, each capped at previewRows tall and stacked on their own
// lines. Non-PNG, unreadable, or oversized files are silently skipped — a preview
// is a nicety, never a reason to lose the chip or the turn. Callers gate this on
// terminal support (see supportsKitty); the returned escapes are inert elsewhere.
func previewEscapes(atts []Attachment) string {
	var previews []string
	for _, a := range atts {
		// f=100 is PNG-only; other formats fall back to the chip alone.
		if a.Mime != "image/png" {
			continue
		}
		data, err := os.ReadFile(a.Path)
		if err != nil || len(data) == 0 || len(data) > maxPreviewBytes {
			continue
		}
		previews = append(previews, kittyPreview(data, previewRows))
	}
	return strings.Join(previews, "\n")
}

// previewRows caps the inline image preview height so a tall image cannot push
// the transcript off-screen (spec: bounded preview height).
const previewRows = 10

// maxPreviewBytes bounds the source size of an inline preview. The kitty escape
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

// kittyGraphicsPrograms names TERM_PROGRAM values whose terminals implement the
// kitty graphics protocol besides kitty itself.
var kittyGraphicsPrograms = map[string]bool{"ghostty": true, "WezTerm": true}

// supportsKitty reports whether the terminal (described by env, an os.Getenv-like
// lookup) renders the kitty graphics protocol, so the composer knows to emit an
// inline preview instead of just a chip. It matches kitty (TERM=*kitty*) and the
// other kitty-graphics terminals by TERM_PROGRAM.
func supportsKitty(env func(string) string) bool {
	if strings.Contains(env("TERM"), "kitty") {
		return true
	}
	return kittyGraphicsPrograms[env("TERM_PROGRAM")]
}

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
