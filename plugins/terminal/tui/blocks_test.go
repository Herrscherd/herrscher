package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestAgentAnswerOpensWithARuleAndIsNotClosed(t *testing.T) {
	out := renderEntry(entry{role: roleAgent, text: "the answer"}, 60, view{tr: newTranslator(), st: testStyles()})
	lines := strings.Split(out, "\n")
	if got := ansi.Strip(lines[0]); !strings.HasPrefix(got, blockIndent+unicodeGlyphs().tool+" the answer") {
		t.Fatalf("the answer must open on the prose gutter: %q", got)
	}
	for _, ln := range lines {
		if strings.Contains(ln, strings.Repeat(unicodeGlyphs().ruleChar, 4)) {
			t.Fatalf("an answer must not be framed by a rule: %q", ln)
		}
	}
}

func TestStreamingAnswerIsLeftOpen(t *testing.T) {
	out := renderEntry(entry{role: roleAgent, text: "half an ans", streaming: true}, 60, view{tr: newTranslator(), st: testStyles()})
	if got := ansi.Strip(out); !strings.HasPrefix(got, blockIndent+unicodeGlyphs().tool+" half an ans") {
		t.Fatalf("a streaming answer keeps the prose gutter: %q", got)
	}
}

func TestNarrowTitledRuleDropsItsTitle(t *testing.T) {
	out := testStyles().titledRule(testStyles().accent, "⚡ Herrscher", 4)
	if strings.Contains(out, "Herrscher") {
		t.Fatalf("a title that does not fit must be dropped, not clipped: %q", out)
	}
	if !strings.Contains(out, unicodeGlyphs().ruleChar) {
		t.Fatalf("what is left must still be a rule: %q", out)
	}
}

func TestGaugeShowsAnySpend(t *testing.T) {
	glyphs := gaugeGlyphsFor(unicodeGlyphs(), true)
	if got := gauge(0.001, testStyles().dim, testStyles().dim, glyphs); !strings.Contains(got, glyphs.gaugeFull) {
		t.Fatalf("a small spend must still fill a cell: %q", got)
	}
	if got := gauge(0, testStyles().dim, testStyles().dim, glyphs); strings.Contains(got, glyphs.gaugeFull) {
		t.Fatalf("nothing spent must read as empty: %q", got)
	}
	if got := gauge(1, testStyles().dim, testStyles().dim, glyphs); strings.Contains(got, glyphs.gaugeEmpty) {
		t.Fatalf("a full window must leave no empty cell: %q", got)
	}
}

func TestGaugeTakesThePalierColour(t *testing.T) {
	if contextGauge(0.995, testStyles()) == "" {
		t.Fatal("a spent context must render a gauge")
	}
	fg := func(r float64) string {
		color, ok := palierStyle(r, testStyles()).GetForeground().(lipgloss.Color)
		if !ok {
			t.Fatalf("palier %v foreground is not a lipgloss.Color", r)
		}
		return string(color)
	}
	if fg(0.99) != string(defaultTheme().Colors.Err) {
		t.Fatalf("a window past the alarm palier must take the Herrscher red: %s", fg(0.99))
	}
	if fg(0.85) != string(defaultTheme().Colors.Wait) || fg(0.1) != string(defaultTheme().Colors.Dim) {
		t.Fatalf("the middle palier warns (%s) and the low one stays dim (%s)", fg(0.85), fg(0.1))
	}
}

func TestEmptyStateListsTheOtherSessions(t *testing.T) {
	m := newTestModel()
	m.ensureTab("a")
	m.ensureTab("b")
	m.active = "a"
	out := m.emptyState(80)
	if !strings.Contains(out, "other sessions") || !strings.Contains(out, "b") {
		t.Fatalf("the other session must be listed: %q", out)
	}
	if strings.Count(out, "\n  ❯") > 0 {
		t.Fatalf("the active session must not list itself: %q", out)
	}
}

func TestEmptyStateOmitsTheListWhenAlone(t *testing.T) {
	m := newTestModel()
	m.ensureTab("a")
	if out := m.emptyState(80); strings.Contains(out, "other sessions") {
		t.Fatalf("a lone session must not be told about its peers: %q", out)
	}
}
