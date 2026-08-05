package tui

import "testing"

func TestThemeGlyphsAndFramesPresent(t *testing.T) {
	for name, g := range map[string]string{
		"tool": glyphTool, "result": glyphResult, "prompt": glyphPrompt,
		"cursor": glyphCursor, "unread": glyphUnread,
	} {
		if g == "" {
			t.Fatalf("glyph %q is empty", name)
		}
	}
	if len(spinFrames) == 0 {
		t.Fatal("spinFrames must be non-empty")
	}
	if accentStyle.Render("x") == "" {
		t.Fatal("accentStyle must render")
	}
	for _, s := range []struct {
		name   string
		render func(...string) string
	}{
		{"dim", dimStyle.Render}, {"user", userStyle.Render}, {"text", textStyle.Render},
		{"green", greenStyle.Render}, {"red", redStyle.Render}, {"spinner", spinnerStyle.Render},
		{"tool", toolStyle.Render}, {"thinking", thinkingStyle.Render},
	} {
		if s.render("x") == "" {
			t.Fatalf("%s style must render", s.name)
		}
	}
}
