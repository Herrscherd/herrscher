package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLegacyKeyTranslatesTheEnhancedReports(t *testing.T) {
	tests := []struct {
		name string
		seq  string
		want string
	}{
		{"shift+enter becomes the newline chord", "\x1b[13;2u", "\x1b\r"},
		{"plain enter stays a carriage return", "\x1b[13u", "\r"},
		{"ctrl+j keeps its control byte", "\x1b[106;5u", "\n"},
		{"alt+b keeps its escape prefix", "\x1b[98;3u", "\x1bb"},
		{"a shifted letter is its capital", "\x1b[97;2u", "A"},
		{"tab, escape and backspace round-trip", "\x1b[9u", "\t"},
		{"an alternate-key variant reads its base", "\x1b[13:13;2u", "\x1b\r"},
		{"a malformed report is dropped, never replayed", "\x1b[;u", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(legacyKey([]byte(tt.seq))); got != tt.want {
				t.Fatalf("legacyKey(%q) = %q, want %q", tt.seq, got, tt.want)
			}
		})
	}
}

func TestFilteredStdinTranslatesEnhancedKeysInStream(t *testing.T) {
	f := &filteredStdin{}
	f.feed([]byte("hi\x1b[13;2uthere\x1b[A"))
	// The arrow survives untouched; only the CSI-u report is rewritten.
	if got := string(f.out); got != "hi\x1b\rthere\x1b[A" {
		t.Fatalf("feed produced %q", got)
	}
}

func TestCtrlWDeletesAWordBeforeItClosesATab(t *testing.T) {
	m := newModel(nil)
	m.ensureTab("c")
	m.input.SetValue("hello world")
	var mm tea.Model = m
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	got := mm.(*model)
	if got.pendingClose {
		t.Fatal("Ctrl+W offered to close the tab while there was a word to delete")
	}
	if v := got.input.Value(); v != "hello " {
		t.Fatalf("input = %q, want the last word deleted", v)
	}
	// With nothing left to delete, the tab is what Ctrl+W is about.
	got.input.SetValue("")
	mm, _ = tea.Model(got).Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if !mm.(*model).pendingClose {
		t.Fatal("Ctrl+W on an empty composer did not offer to close the tab")
	}
}

func TestWheelScrollsTheTranscript(t *testing.T) {
	m := newTestModel()
	tb := m.ensureTab("c")
	for i := 0; i < 200; i++ {
		tb.appendEntry(entry{role: roleNotice, text: "line"})
	}
	var mm tea.Model = m
	mm, _ = mm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(*model)
	m.syncViewport()
	if m.vp.AtTop() {
		t.Fatal("a 200-line transcript did not overflow an 24-row viewport")
	}
	before := m.vp.YOffset
	mm, _ = mm.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if got := mm.(*model).vp.YOffset; got >= before {
		t.Fatalf("wheel-up left the transcript at %d (was %d) — the alt screen has no scrollback to fall back on", got, before)
	}
}
