package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestAgentAnswerOpensWithARuleAndIsNotClosed(t *testing.T) {
	out := renderEntry(entry{role: roleAgent, text: "the answer"}, 60, view{st: testStyles()})
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[0], unicodeGlyphs().agentTitle()) {
		t.Fatalf("the opening rule must name whose voice this is: %q", lines[0])
	}
	if last := lines[len(lines)-1]; strings.Contains(last, strings.Repeat(unicodeGlyphs().ruleChar, 4)) {
		t.Fatalf("a finished answer must not be closed off by a rule: %q", last)
	}
	if !strings.Contains(out, blockIndent+"the answer") {
		t.Fatalf("the body must sit in the block gutter: %q", out)
	}
}

func TestStreamingAnswerIsLeftOpen(t *testing.T) {
	out := renderEntry(entry{role: roleAgent, text: "half an ans", streaming: true}, 60, view{st: testStyles()})
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[0], unicodeGlyphs().agentTitle()) {
		t.Fatalf("a streaming block still announces itself: %q", out)
	}
	if strings.Contains(lines[len(lines)-1], strings.Repeat(unicodeGlyphs().ruleChar, 4)) {
		t.Fatalf("a streaming block must not be closed off: %q", out)
	}
}

func TestNarrowTitledRuleDropsItsTitle(t *testing.T) {
	out := testStyles().titledRule(testStyles().accent, unicodeGlyphs().agentTitle(), 4)
	if strings.Contains(out, "Herrscher") {
		t.Fatalf("a title that does not fit must be dropped, not clipped: %q", out)
	}
	if !strings.Contains(out, unicodeGlyphs().ruleChar) {
		t.Fatalf("what is left must still be a rule: %q", out)
	}
}

func TestGaugeShowsAnySpend(t *testing.T) {
	if got := gauge(0.001, testStyles().dim, testStyles().dim, unicodeGlyphs()); !strings.Contains(got, unicodeGlyphs().gaugeFull) {
		t.Fatalf("a small spend must still fill a cell: %q", got)
	}
	if got := gauge(0, testStyles().dim, testStyles().dim, unicodeGlyphs()); strings.Contains(got, unicodeGlyphs().gaugeFull) {
		t.Fatalf("nothing spent must read as empty: %q", got)
	}
	if got := gauge(1, testStyles().dim, testStyles().dim, unicodeGlyphs()); strings.Contains(got, unicodeGlyphs().gaugeEmpty) {
		t.Fatalf("a full window must leave no empty cell: %q", got)
	}
}

func TestGaugeTakesThePalierColour(t *testing.T) {
	alarm := renderGauge(199_000, "claude", 120, testStyles())
	calm := renderGauge(1_000, "claude", 120, testStyles())
	if alarm == "" || calm == "" {
		t.Fatal("a spent context must render a gauge")
	}
	if alarm == calm {
		t.Fatal("a full window and an empty one must not render alike")
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
	if !strings.Contains(out, "autres sessions") || !strings.Contains(out, "b") {
		t.Fatalf("the other session must be listed: %q", out)
	}
	if strings.Count(out, "\n  ❯") > 0 {
		t.Fatalf("the active session must not list itself: %q", out)
	}
}

func TestEmptyStateOmitsTheListWhenAlone(t *testing.T) {
	m := newTestModel()
	m.ensureTab("a")
	if out := m.emptyState(80); strings.Contains(out, "autres sessions") {
		t.Fatalf("a lone session must not be told about its peers: %q", out)
	}
}
