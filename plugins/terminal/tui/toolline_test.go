package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func transcriptWidths() []int {
	return []int{60, 80, 120, 200}
}

func toolTestView(expand bool) view {
	return view{caps: Capabilities{Colour: ColourTrue}, st: testStyles(), spin: "⣾", expand: expand}
}

func assertFitsWidth(t *testing.T, out string, width int) {
	t.Helper()
	for _, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w > width {
			t.Fatalf("width %d: line is %d cols: %q", width, w, ansi.Strip(ln))
		}
	}
}

func TestToolHeadKeepsVerbAndStateAtEveryWidth(t *testing.T) {
	item := entry{role: roleTool, text: "Edit internal/very/long/path/to/some/deeply/nested/file.go", span: 1200 * time.Millisecond}
	for _, width := range transcriptWidths() {
		out := renderEntry(item, width, toolTestView(false))
		assertFitsWidth(t, out, width)
		head := ansi.Strip(strings.Split(out, "\n")[0])
		if !strings.Contains(head, "Edit") || !strings.Contains(head, "✓") {
			t.Fatalf("width %d: verb or state lost: %q", width, head)
		}
	}
}

func TestToolHeadRightAlignsDurationAndState(t *testing.T) {
	item := entry{role: roleTool, text: "Read tui/tui.go", span: 100 * time.Millisecond}
	head := ansi.Strip(renderTool(item, 120, toolTestView(false)))
	if !strings.HasSuffix(head, "0.1s ✓") {
		t.Fatalf("duration and state must sit on the right edge: %q", head)
	}
	if lipgloss.Width(head) != 120 {
		t.Fatalf("right side must reach the edge: %d cols", lipgloss.Width(head))
	}
}

func TestToolHeadShowsRunningSpinnerAndFailure(t *testing.T) {
	live := ansi.Strip(renderTool(entry{role: roleTool, text: "Bash go test", live: true}, 80, toolTestView(false)))
	if !strings.Contains(live, "⣾") {
		t.Fatalf("a running tool shows the spinner: %q", live)
	}
	failed := ansi.Strip(renderTool(entry{role: roleTool, text: "Bash go test", failed: true}, 80, toolTestView(false)))
	if !strings.Contains(failed, unicodeGlyphs().failure) {
		t.Fatalf("a failed tool shows the cross: %q", failed)
	}
}

func TestToolResultsCollapseToTwoLinesAndOpen(t *testing.T) {
	text := "Bash go test\nline one\nline two\nline three\nline four"
	closed := ansi.Strip(renderTool(entry{role: roleTool, text: text, foldable: true}, 80, toolTestView(false)))
	if strings.Contains(closed, "line one") {
		t.Fatalf("a closed tool hides its results: %q", closed)
	}
	open := ansi.Strip(renderTool(entry{role: roleTool, text: text, foldable: true, open: true}, 80, toolTestView(false)))
	if !strings.Contains(open, "    "+unicodeGlyphs().result+" line one") || !strings.Contains(open, "+2") {
		t.Fatalf("an open tool shows two results and the hidden count: %q", open)
	}
	expanded := ansi.Strip(renderTool(entry{role: roleTool, text: text, foldable: true, open: true}, 80, toolTestView(true)))
	if !strings.Contains(expanded, "line four") {
		t.Fatalf("alt+e must show every result: %q", expanded)
	}
}

const editBody = "Edit tui/tui.go\n@@ -10,3 +10,4 @@ func f()\n ctx\n-old\n+new one\n+new two\n tail"

func TestDiffNumbersInsertionsAndShiftsLaterLines(t *testing.T) {
	out := ansi.Strip(renderTool(entry{role: roleTool, text: editBody, open: true}, 120, toolTestView(true)))
	for _, want := range []string{"ctx", "+ new one", "+ new two", "tail"} {
		if !strings.Contains(out, want) {
			t.Fatalf("diff row %q missing: %q", want, out)
		}
	}
	if !strings.Contains(out, "+2 −1") {
		t.Fatalf("header must count the additions and deletions: %q", out)
	}
	if !strings.Contains(out, "⋯") || !strings.Contains(out, "@@ func f()") {
		t.Fatalf("hunk separator missing: %q", out)
	}
}

func TestDiffHasTwoNumberColumnsWideAndOneNarrow(t *testing.T) {
	wide := ansi.Strip(renderDiff("@@ -10,3 +10,4 @@\n+new one", 120, toolTestView(false)))
	if !strings.Contains(wide, "        10 + new one") {
		t.Fatalf("120 columns must draw old and new columns: %q", wide)
	}
	narrow := ansi.Strip(renderDiff("@@ -10,3 +10,4 @@\n+new one", 60, toolTestView(false)))
	if !strings.Contains(narrow, "│   10 + new one") {
		t.Fatalf("under 70 columns the diff draws one column: %q", narrow)
	}
}

func TestDiffFitsEveryTranscriptWidth(t *testing.T) {
	long := "Edit tui/tui.go\n@@ -1 +1 @@ func x()\n+" + strings.Repeat("x", 300) + "\n-short"
	for _, width := range transcriptWidths() {
		out := renderEntry(entry{role: roleTool, text: long, open: true}, width, toolTestView(false))
		assertFitsWidth(t, out, width)
	}
}

func TestDiffFoldsAfterFourLinesUnlessExpanded(t *testing.T) {
	body := "Edit a.go\n@@ -1,6 +1,6 @@\n+a\n+b\n+c\n+d\n+e\n+f"
	folded := ansi.Strip(renderTool(entry{role: roleTool, text: body, open: true}, 120, toolTestView(false)))
	if !strings.Contains(folded, "2 lignes de plus (alt+e)") {
		t.Fatalf("a diff past four lines folds with the count: %q", folded)
	}
	expanded := ansi.Strip(renderTool(entry{role: roleTool, text: body, open: true}, 120, toolTestView(true)))
	if strings.Contains(expanded, "lignes de plus") || !strings.Contains(expanded, "+ f") {
		t.Fatalf("alt+e must lift the fold: %q", expanded)
	}
}

func TestThinkingFoldedShowsLabelAndHint(t *testing.T) {
	item := entry{role: roleThinking, text: "weighing two options", span: 14 * time.Second, tokens: 1200}
	for _, width := range transcriptWidths() {
		out := renderThinking(item, width, toolTestView(false))
		assertFitsWidth(t, out, width)
	}
	got := ansi.Strip(renderThinking(item, 120, toolTestView(false)))
	for _, want := range []string{"▸", "Réflexion · 14s · 1.2k tokens", "ctrl+o"} {
		if !strings.Contains(got, want) {
			t.Fatalf("folded thinking missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "weighing") {
		t.Fatalf("folded thinking must hide its text: %q", got)
	}
}

func TestThinkingUnfoldedShowsTheText(t *testing.T) {
	item := entry{role: roleThinking, text: "weighing two options", span: 14 * time.Second, open: true}
	got := ansi.Strip(renderThinking(item, 80, toolTestView(false)))
	if !strings.Contains(got, "▾") || !strings.Contains(got, "weighing two options") {
		t.Fatalf("unfolded thinking must show its text: %q", got)
	}
	if strings.Contains(got, "tokens") {
		t.Fatalf("no token count without a reported figure: %q", got)
	}
}

func TestSubagentTreeMarksEachState(t *testing.T) {
	now := time.Now()
	agents := []liveAgent{
		{Subagent: contracts.Subagent{ID: "a", Name: "explore", Kind: "claude", State: stateDone}, since: now.Add(-5 * time.Second), ended: now.Add(-1 * time.Second)},
		{Subagent: contracts.Subagent{ID: "b", Name: "tests", Kind: "codex", State: "running"}, since: now.Add(-8 * time.Second)},
		{Subagent: contracts.Subagent{ID: "c", Name: "review", Kind: "gemini", State: stateTimeout}, since: now.Add(-20 * time.Second), ended: now.Add(-2 * time.Second)},
	}
	for _, width := range transcriptWidths() {
		rows := subagentRows(agents, now, width, toolTestView(false))
		for _, row := range rows {
			if lipgloss.Width(row) > width {
				t.Fatalf("width %d: subagent row is %d cols: %q", width, lipgloss.Width(row), ansi.Strip(row))
			}
		}
	}
	rows := make([]string, 0, len(agents))
	for _, row := range subagentRows(agents, now, 120, toolTestView(false)) {
		rows = append(rows, ansi.Strip(row))
	}
	for index, want := range []string{"├", "├", "└"} {
		if !strings.Contains(rows[index], want) {
			t.Fatalf("row %d must carry %q: %q", index, want, rows[index])
		}
	}
	if !strings.Contains(rows[0], unicodeGlyphs().success) || !strings.Contains(rows[1], "⣾") || !strings.Contains(rows[2], unicodeGlyphs().failure) {
		t.Fatalf("states must show check, spinner and cross: %q", rows)
	}
	if !strings.Contains(rows[2], subagentRetryNote) {
		t.Fatalf("a timed out agent must say it was retried: %q", rows[2])
	}
}

func TestBusyLineCarriesVerbTimeTokensCostAndEscape(t *testing.T) {
	for _, width := range transcriptWidths() {
		out := busyLine(width, testStyles(), "⣾", "1m12s", "4.2k", "$0.31")
		assertFitsWidth(t, out, width)
	}
	got := ansi.Strip(busyLine(120, testStyles(), "⣾", "1m12s", "4.2k", "$0.31"))
	for _, want := range []string{workingVerb, "1m12s", "↑ 4.2k", "$0.31", "Échap interrompre"} {
		if !strings.Contains(got, want) {
			t.Fatalf("busy line %q missing %q", got, want)
		}
	}
	if !strings.HasSuffix(got, "Échap interrompre") {
		t.Fatalf("the escape hint must sit on the right: %q", got)
	}
}
