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
	if len(spinFrames()) == 0 {
		t.Fatal("spinFrames() must be non-empty")
	}
	if newStyles().accent.Render("x") == "" {
		t.Fatal("newStyles().accent must render")
	}
	for _, s := range []struct {
		name   string
		render func(...string) string
	}{
		{"dim", newStyles().dim.Render},
		{"user", newStyles().user.Render},
		{"text", newStyles().text.Render},
		{"green", newStyles().green.Render},
		{"red", newStyles().red.Render},
		{"spinner", newStyles().spinner.Render},
		{"tool", newStyles().tool.Render},
		{"thinking", newStyles().thinking.Render},
	} {
		if s.render("x") == "" {
			t.Fatalf("%s style must render", s.name)
		}
	}
}
