package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestResultLineIndentsUnderTheToolGlyph(t *testing.T) {
	got := ansi.Strip(resultLine("214 lignes", testStyles().dim, unicodeGlyphs()))
	if !strings.HasPrefix(got, "  "+unicodeGlyphs().result+" ") {
		t.Fatalf("result line = %q", got)
	}
}

func TestThinkingLineDropsTheWordThinking(t *testing.T) {
	got := ansi.Strip(thinkingLine("le hook est global", unicodeGlyphs()))
	if strings.Contains(got, "Thinking") {
		t.Fatalf("thinking line still labelled: %q", got)
	}
	if !strings.HasPrefix(got, unicodeGlyphs().thinking+" ") {
		t.Fatalf("thinking line = %q", got)
	}
}

func TestSpinnerHintCarriesVerbDurationAndEscape(t *testing.T) {
	m := sizedTestModel(t, 74)
	tb := &tab{busy: true, startedAt: time.Now().Add(-12 * time.Second)}
	got := ansi.Strip(m.spinnerHint(tb))
	for _, want := range []string{workingVerb, "12s", "échap pour interrompre"} {
		if !strings.Contains(got, want) {
			t.Fatalf("spinner hint %q missing %q", got, want)
		}
	}
}
