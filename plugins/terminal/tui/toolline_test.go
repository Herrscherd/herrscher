package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func plainLines(s string) []string {
	out := strings.Split(ansi.Strip(s), "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	return out
}

func TestToolEntryRendersBulletAndResult(t *testing.T) {
	out := renderEntry(entry{role: roleTool, text: "Bash(ls build/)\nremoved 3 files"}, 60, view{st: testStyles()})
	if !strings.Contains(out, unicodeGlyphs().tool+" Bash(ls build/)") {
		t.Fatalf("tool call must render `● Bash(ls build/)`: %q", out)
	}
	if !strings.Contains(out, unicodeGlyphs().result+" removed 3 files") {
		t.Fatalf("tool result must render `⎿ removed 3 files`: %q", out)
	}
}

func TestToolVerbSplitsKnownTools(t *testing.T) {
	cases := []struct {
		in        string
		fam, rest string
	}{
		{"Read core/parse.go", familyRead, "core/parse.go"},
		{"bash go test ./...", familyRun, "go test ./..."},
		{"Grep errPrefix core/", familySearch, "errPrefix core/"},
		{"WebFetch https://x.dev", familyWeb, "https://x.dev"},
		{"Task review the diff", familyAgent, "review the diff"},
		{"Edit core/parse.go", familyWrite, "core/parse.go"},
		{"Reading the envfile", "", "Reading the envfile"},
		{"bash", familyRun, ""},
		{"", "", ""},
		{"   ", "", ""},
	}
	for _, c := range cases {
		fam, rest := toolVerb(c.in)
		if fam != c.fam || rest != c.rest {
			t.Errorf("toolVerb(%q) = (%q, %q), want (%q, %q)", c.in, fam, rest, c.fam, c.rest)
		}
	}
}

func TestTypedToolRendersItsFamilyGlyph(t *testing.T) {
	out := renderEntry(entry{role: roleTool, text: "Read core/parse.go"}, 60, view{st: testStyles()})
	if !strings.Contains(out, unicodeGlyphs().familyGlyph(familyRead)) {
		t.Fatalf("a read must carry the read glyph: %q", out)
	}
	if !strings.Contains(out, "core/parse.go") {
		t.Fatalf("a tool line must keep its target: %q", out)
	}
	unknown := renderEntry(entry{role: roleTool, text: "pondering deeply"}, 60, view{st: testStyles()})
	if !strings.Contains(unknown, unicodeGlyphs().tool+" pondering deeply") {
		t.Fatalf("an unknown tool falls back to the generic bullet: %q", unknown)
	}
}

func TestAbbreviatePathsShortensDeepAbsolutePaths(t *testing.T) {
	cases := []struct{ in, want string }{
		{
			"cd /home/shan/dev/enderbot/.herrscher-sessions/u03787009/ch-8688/core",
			"cd …/ch-8688/core",
		},
		{"cd /a/b/c/d; ls", "cd …/c/d; ls"},
		{"(/a/b/c/d)", "(…/c/d)"},
		{`"/a/b/c/d"`, `"…/c/d"`},
		{"/a/b/c/d", "…/c/d"},
		{"/home/shan/dev", "/home/shan/dev"},
		{"/etc/hosts", "/etc/hosts"},
		{"~/a/b/c/d", "…/c/d"},
		{"~/dev/herrscher", "~/dev/herrscher"},
		{"sed -n '1,120p' src/common/utils/Components/Modal/Modal.ts", "sed -n '1,120p' src/common/utils/Components/Modal/Modal.ts"},
		{"grep -rln modal: src/modules --include=*.ts", "grep -rln modal: src/modules --include=*.ts"},
		{"cd  /a/b/c/d   ls", "cd  …/c/d   ls"},
		{"", ""},
	}
	for _, c := range cases {
		if got := abbreviatePaths(c.in); got != c.want {
			t.Errorf("abbreviatePaths(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToolLineHangsContinuationsUnderTheTarget(t *testing.T) {
	out := renderTool("bash grep -n needle one two three four five six seven", 40, view{st: testStyles(), expand: true})
	lines := plainLines(out)
	if len(lines) < 2 {
		t.Fatalf("the command must fold at width 40: %q", lines)
	}
	if !strings.HasPrefix(lines[1], strings.Repeat(" ", toolTargetColumn)) {
		t.Fatalf("a continuation must hang under the target column:\n%q", lines)
	}
	if strings.HasPrefix(lines[1], strings.Repeat(" ", toolTargetColumn+1)) {
		t.Fatalf("a continuation must hang exactly at the target column:\n%q", lines)
	}
}

func TestUnknownToolLineHangsUnderItsBullet(t *testing.T) {
	out := renderTool("pondering one two three four five six seven eight nine ten", 40, view{st: testStyles(), expand: true})
	lines := plainLines(out)
	if len(lines) < 2 {
		t.Fatalf("the line must fold at width 40: %q", lines)
	}
	if !strings.HasPrefix(lines[1], "  ") || strings.HasPrefix(lines[1], "   ") {
		t.Fatalf("an unknown tool hangs under its bullet, two cells in:\n%q", lines)
	}
}

func TestToolLineIsCappedWithACount(t *testing.T) {
	long := "bash cd /a/b/c/d; echo one two three four five six seven eight nine ten eleven twelve thirteen fourteen"
	lines := plainLines(renderTool(long, 40, view{st: testStyles()}))
	if len(lines) != toolLineCap {
		t.Fatalf("a long command is capped at %d lines, got %d:\n%q", toolLineCap, len(lines), lines)
	}
	if !strings.Contains(lines[len(lines)-1], unicodeGlyphs().moreLines) {
		t.Fatalf("a capped line must say what it kept back:\n%q", lines)
	}
	if !strings.Contains(lines[len(lines)-1], "+") {
		t.Fatalf("the marker must carry a count:\n%q", lines)
	}
	for _, ln := range lines {
		if w := ansi.StringWidth(ln); w > 40 {
			t.Fatalf("a capped line must stay inside the width, got %d: %q", w, ln)
		}
	}
}

func TestShortToolLineCarriesNoMarker(t *testing.T) {
	out := renderTool("Read core/parse.go", 60, view{st: testStyles()})
	if strings.Contains(out, unicodeGlyphs().moreLines) {
		t.Fatalf("a line under the cap must carry no marker: %q", out)
	}
	if lines := plainLines(out); len(lines) != 1 {
		t.Fatalf("a short line stays one line: %q", lines)
	}
}

func TestExpandedToolLineKeepsTheWholeCommand(t *testing.T) {
	const cmd = "bash cd /home/shan/dev/enderbot/.herrscher-sessions/u03787009/ch-8688/core; sed -n '1,120p' src/common/utils/Components/Modal/Modal.ts"
	expanded := plainLines(renderTool(cmd, 40, view{st: testStyles(), expand: true}))
	joined := strings.Join(expanded, "")
	squashed := strings.ReplaceAll(joined, " ", "")
	if !strings.Contains(squashed, "/home/shan/dev/enderbot/.herrscher-sessions/u03787009/ch-8688/core") {
		t.Fatalf("an expanded line keeps its path whole:\n%q", expanded)
	}
	if strings.Contains(joined, "…") {
		t.Fatalf("an expanded line abbreviates nothing:\n%q", expanded)
	}
	if len(expanded) <= toolLineCap {
		t.Fatalf("an expanded line is not capped, got %d lines:\n%q", len(expanded), expanded)
	}
	if strings.Contains(joined, unicodeGlyphs().moreLines) {
		t.Fatalf("an expanded line kept nothing back, so it must not claim to:\n%q", expanded)
	}
}

func TestNarrowToolLineStaysLegible(t *testing.T) {
	lines := plainLines(renderTool("bash go test ./...", 12, view{st: testStyles(), expand: true}))
	if strings.Join(lines, "") == "" {
		t.Fatal("a narrow window must still render the line")
	}
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			t.Fatalf("a narrow render must not produce blank rows:\n%q", lines)
		}
	}
	if !strings.Contains(strings.Join(lines, ""), "test") {
		t.Fatalf("a narrow render must keep the command:\n%q", lines)
	}
}

func TestToolResultTakesTheSameBudget(t *testing.T) {
	entryText := "Read core/parse.go\n" + strings.Repeat("a long summary of what came back ", 6)
	lines := plainLines(renderTool(entryText, 40, view{st: testStyles()}))
	if len(lines) != 1+toolLineCap {
		t.Fatalf("the head plus a capped result is %d lines, got %d:\n%q", 1+toolLineCap, len(lines), lines)
	}
	if !strings.Contains(lines[1], unicodeGlyphs().result) {
		t.Fatalf("the result keeps its own mark:\n%q", lines)
	}
	if !strings.Contains(lines[len(lines)-1], unicodeGlyphs().moreLines) {
		t.Fatalf("a capped result says what it kept back:\n%q", lines)
	}
}

func toolModel(t *testing.T) *model {
	t.Helper()
	m := readyModel(&fakeBackend{})
	tb := m.ensureTab("a")
	m.active = "a"
	tb.appendEntry(entry{role: roleTool, text: "bash cd /home/shan/dev/enderbot/.herrscher-sessions/u03787009/ch-8688/core; sed -n '1,120p' src/common/utils/Components/Modal/Modal.ts; grep -n getValue src/common/commands/context/ModalContext.ts"})
	m.syncViewport()
	return m
}

func TestToggleToolExpandIsReversible(t *testing.T) {
	m := toolModel(t)
	before := m.thinkingContent()
	m.toggleToolExpand()
	after := m.thinkingContent()
	if after == before {
		t.Fatal("expanding must change what is shown")
	}
	if !strings.Contains(ansi.Strip(after), ".herrscher-sessions") {
		t.Fatalf("an expanded transcript spells the path out:\n%s", ansi.Strip(after))
	}
	if strings.Contains(ansi.Strip(before), ".herrscher-sessions") {
		t.Fatalf("a budgeted transcript abbreviates it:\n%s", ansi.Strip(before))
	}
	m.toggleToolExpand()
	if m.thinkingContent() != before {
		t.Fatal("collapsing must restore the transcript exactly")
	}
}

func TestAltEExpandsToolLines(t *testing.T) {
	m := toolModel(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}, Alt: true})
	if !m.expandTools {
		t.Fatal("alt+e must lift the tool-line budget")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}, Alt: true})
	if m.expandTools {
		t.Fatal("alt+e must put it back")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if m.expandTools {
		t.Fatal("ctrl+e belongs to the composer, not to the transcript")
	}
}
