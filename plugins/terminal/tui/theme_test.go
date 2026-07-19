package tui

import "testing"

func TestThemeGlyphsAndFramesPresent(t *testing.T) {
	for name, g := range map[string]string{
		"brand": glyphBrand, "agent": glyphAgent, "you": glyphYou,
		"thinking": glyphThinking, "busy": glyphBusy, "unread": glyphUnread,
	} {
		if g == "" {
			t.Fatalf("glyph %q is empty", name)
		}
	}
	if len(spinFrames) == 0 {
		t.Fatal("spinFrames must be non-empty")
	}
	if humanStyle.Render("x") == "" {
		t.Fatal("humanStyle must render")
	}
}
