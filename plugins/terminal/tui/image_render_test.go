package tui

import (
	"strings"
	"testing"
)

// TestRenderEntryEmitsPreviewUnderChip verifies a user entry carrying a
// precomputed kitty preview renders it on its own line after the chip, and that
// an entry without one emits no graphics escape.
func TestRenderEntryEmitsPreviewUnderChip(t *testing.T) {
	att := Attachment{Name: "shot.png", Path: "/tmp/shot.png", Mime: "image/png", Size: 2048}
	preview := kittyPreview([]byte("PNGDATA"), previewRows)

	withPreview := renderEntry(entry{role: roleYou, text: "look", attachments: []Attachment{att}, preview: preview}, 40)
	if !strings.Contains(withPreview, preview) {
		t.Fatalf("entry with a preview must emit the kitty escape")
	}
	// The chip still renders above the preview.
	chipIdx := strings.Index(withPreview, "shot.png")
	previewIdx := strings.Index(withPreview, preview)
	if chipIdx < 0 || previewIdx < 0 || previewIdx < chipIdx {
		t.Fatalf("preview must appear after the chip (chip=%d preview=%d)", chipIdx, previewIdx)
	}

	without := renderEntry(entry{role: roleYou, text: "look", attachments: []Attachment{att}}, 40)
	if strings.Contains(without, "\x1b_G") {
		t.Fatalf("entry without a preview must not emit a graphics escape")
	}
}
