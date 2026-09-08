package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestResultLineIndentsUnderTheToolGlyph(t *testing.T) {
	got := ansi.Strip(resultLine("214 lignes", dimStyle))
	if !strings.HasPrefix(got, "  "+glyphResult+" ") {
		t.Fatalf("result line = %q", got)
	}
}

func TestThinkingLineDropsTheWordThinking(t *testing.T) {
	got := ansi.Strip(thinkingLine("le hook est global"))
	if strings.Contains(got, "Thinking") {
		t.Fatalf("thinking line still labelled: %q", got)
	}
	if !strings.HasPrefix(got, glyphThinking+" ") {
		t.Fatalf("thinking line = %q", got)
	}
}

func TestSpinnerHintCarriesVerbDurationAndEscape(t *testing.T) {
	m := sizedTestModel(t, 74)
	tb := &tab{busy: true, startedAt: time.Now().Add(-12 * time.Second)}
	got := ansi.Strip(m.spinnerHint(tb))
	for _, want := range []string{workingVerb, "12s", "esc to interrupt"} {
		if !strings.Contains(got, want) {
			t.Fatalf("spinner hint %q missing %q", got, want)
		}
	}
}
