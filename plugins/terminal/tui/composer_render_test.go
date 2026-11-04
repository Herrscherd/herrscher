package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const composerRenderRows = 20

func renderAt(t *testing.T, width int, prepare func(m *model)) []string {
	t.Helper()
	m, _ := queueFixture(t)
	m.Update(tea.WindowSizeMsg{Width: width, Height: composerRenderRows})
	if prepare != nil {
		prepare(m)
	}
	view := m.View()
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > width {
			t.Fatalf("width %d: line %d is %d cols: %q", width, i, w, ansi.Strip(line))
		}
	}
	if len(lines) != composerRenderRows {
		t.Fatalf("width %d: view has %d rows, want %d", width, len(lines), composerRenderRows)
	}
	return lines
}

func TestRenderFitsWidthsAndHeight(t *testing.T) {
	widths := []int{60, 80, 120, 200}
	for _, w := range widths {
		renderAt(t, w, nil)
	}
}

func TestRenderWithQueueFitsWidthsAndHeight(t *testing.T) {
	for _, w := range []int{60, 80, 120, 200} {
		renderAt(t, w, func(m *model) {
			m.tabs["a"].busy = true
			m.tabs["a"].queue = []queuedInput{
				{text: "une très longue demande qui doit être tronquée proprement pour tenir dans la largeur"},
				{text: "réoriente", steer: true},
			}
			m.applySize()
			m.syncViewport()
		})
	}
}

func TestRenderShellStateShowsWarnTitleAndShellFooter(t *testing.T) {
	lines := renderAt(t, 120, func(m *model) {
		typeLine(m, "!ls")
	})
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "! SHELL") {
		t.Fatal("shell state must show the ! SHELL title")
	}
	if !strings.Contains(joined, "la commande passe par la politique d’outils de l’hôte") {
		t.Fatal("shell state must show the shell footer")
	}
}

func TestRenderEditingStateShowsEditorTitle(t *testing.T) {
	lines := renderAt(t, 120, func(m *model) {
		m.editing = true
	})
	if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "Éditeur") {
		t.Fatal("editing state must show the Éditeur title")
	}
}

func TestRenderBusyFooterOffersQueue(t *testing.T) {
	lines := renderAt(t, 200, func(m *model) {
		m.tabs["a"].busy = true
	})
	if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "⌃⏎ envoyer la file") {
		t.Fatal("busy footer must mention ctrl+enter")
	}
}

func TestComposerStyleFollowsState(t *testing.T) {
	m, _ := queueFixture(t)
	if m.composerStyle().Render("x") != m.st.dim.Render("x") {
		t.Fatal("idle composer uses the dim border")
	}
	typeLine(m, "!ls")
	if m.composerStyle().Render("x") != m.st.warn.Render("x") {
		t.Fatal("shell composer uses the warn border")
	}
	typeLine(m, "")
	m.editing = true
	if m.composerStyle().Render("x") != m.st.violet.Render("x") {
		t.Fatal("editing composer uses the violet border")
	}
}

func TestShellTitleNamesTheSessionDirectory(t *testing.T) {
	m, f := queueFixture(t)
	f.sessions[0].Dir = "/srv/projets/herrscher/.worktrees/tui-v2"
	m.infoAt = time.Time{}
	typeLine(m, "!ls")
	title := ansi.Strip(m.composerTitleLeft())
	if !strings.Contains(title, "! SHELL · dans .worktrees/tui-v2 · la sortie entre dans la session") {
		t.Fatalf("shell title must name the session directory, got %q", title)
	}
}

func TestShellTitleDropsTheDirectoryWhenUnknown(t *testing.T) {
	m, _ := queueFixture(t)
	typeLine(m, "!ls")
	title := ansi.Strip(m.composerTitleLeft())
	if strings.Contains(title, "· dans ") {
		t.Fatalf("an unknown directory must leave no « dans » segment, got %q", title)
	}
	if !strings.Contains(title, "la sortie entre dans la session") {
		t.Fatalf("shell title must keep its explanation, got %q", title)
	}
}

func TestShellPromptShowsTheDollarAndTextTurnsWarn(t *testing.T) {
	m, _ := queueFixture(t)
	typeLine(m, "!ls")
	m.syncComposer()
	m.buildFrame()
	if got := ansi.Strip(m.inputRow()); !strings.HasPrefix(got, "$ !ls") {
		t.Fatalf("shell mode shows the $ prompt and the typed bang, got %q", got)
	}
	if m.input.FocusedStyle.Text.GetForeground() != m.st.warn.GetForeground() {
		t.Fatal("shell mode must colour the typed text with the warn style")
	}
	typeLine(m, "ls")
	m.syncComposer()
	m.buildFrame()
	if m.input.FocusedStyle.Text.GetForeground() == m.st.warn.GetForeground() {
		t.Fatal("leaving shell mode must restore the normal text colour")
	}
}

func TestComposerFrameColoursFollowShellAndEditor(t *testing.T) {
	m, _ := queueFixture(t)
	typeLine(m, "!ls")
	if m.composerStyle().GetForeground() != m.st.warn.GetForeground() {
		t.Fatal("shell mode must draw the frame in the warn colour")
	}
	typeLine(m, "")
	m.editing = true
	if m.composerStyle().GetForeground() != m.st.violet.GetForeground() {
		t.Fatal("editor mode must draw the frame in violet")
	}
}

func (m *model) composerTitleLeft() string {
	left, _ := m.composerTitles()
	return strings.Join(left, "")
}

func composerEdgeLines(lines []string, corner string) []string {
	var edges []string
	for _, line := range lines {
		if stripped := ansi.Strip(line); strings.Contains(stripped, corner) {
			edges = append(edges, stripped)
		}
	}
	return edges
}

func TestShellTopEdgeFoldsWholeSegmentsAtSixtyColumns(t *testing.T) {
	m, f := queueFixture(t)
	f.sessions[0].Dir = "/home/shan/dev/herrscher/.worktrees/tui-v2"
	m.Update(tea.WindowSizeMsg{Width: 60, Height: composerRenderRows})
	m.infoAt = time.Time{}
	typeLine(m, "!ls")
	top := ansi.Strip(m.composerZone()[0])
	if !strings.Contains(top, "! SHELL · dans .worktrees/tui-v2") {
		t.Fatalf("the directory segment must survive folding at 60 columns, got %q", top)
	}
	if strings.Contains(top, "sortie") {
		t.Fatalf("the explanation segment must fold away whole, got %q", top)
	}
	if ansi.StringWidth(top) != 60 {
		t.Fatalf("the top edge must keep the window width, got %d", ansi.StringWidth(top))
	}
}

func TestIdleFooterFoldsWholeSegmentsAgainstTheBorder(t *testing.T) {
	lines := renderAt(t, 60, nil)
	edges := composerEdgeLines(lines, "╰")
	if len(edges) == 0 {
		t.Fatal("the composer bottom edge must be visible at 60 columns")
	}
	edge := edges[len(edges)-1]
	if !strings.Contains(edge, "╰ ⏎ envoyer · @ fichier · / commande ") {
		t.Fatalf("the idle footer must keep whole segments, got %q", edge)
	}
	if !strings.HasSuffix(edge, "─ échap échap retour ╯") {
		t.Fatalf("the right label must keep its rule and sit on the border, got %q", edge)
	}
}

func TestBusyFooterAtSixtyColumnsFoldsBeforeTheInterruptHint(t *testing.T) {
	lines := renderAt(t, 60, func(m *model) {
		m.tabs["a"].busy = true
	})
	edges := composerEdgeLines(lines, "╰")
	edge := edges[len(edges)-1]
	if !strings.Contains(edge, "╰ ⏎ en file · ⌥⏎ réorienter ") {
		t.Fatalf("the busy footer must keep its first segments, got %q", edge)
	}
	if !strings.HasSuffix(edge, "─ échap interrompre ╯") {
		t.Fatalf("the busy footer must keep échap interrompre on the border, got %q", edge)
	}
}

func TestBusyShellFooterKeepsInterruptHint(t *testing.T) {
	lines := renderAt(t, 120, func(m *model) {
		m.tabs["a"].busy = true
		typeLine(m, "!ls")
	})
	edges := composerEdgeLines(lines, "╰")
	if len(edges) == 0 || !strings.Contains(edges[len(edges)-1], "échap interrompre") {
		t.Fatalf("a busy shell composer must show échap interrompre on its footer, got %q", edges)
	}
}

func TestEditorTitleNamesTheEditorLaunched(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "/usr/bin/nvim")
	m, _ := queueFixture(t)
	m.editing = true
	title := ansi.Strip(m.composerTitleLeft())
	if !strings.HasSuffix(title, "brouillon ouvert dans nvim") {
		t.Fatalf("the editor title must name the editor really launched, got %q", title)
	}
}

func TestEditorTitleFallsBackToViWithoutEditorVariables(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	m, _ := queueFixture(t)
	m.editing = true
	if title := ansi.Strip(m.composerTitleLeft()); !strings.HasSuffix(title, "brouillon ouvert dans vi") {
		t.Fatalf("without EDITOR or VISUAL the title must name vi, got %q", title)
	}
}

func TestEditorNameFollowsTheLauncherOrder(t *testing.T) {
	t.Setenv("EDITOR", "/usr/bin/nvim")
	t.Setenv("VISUAL", "/usr/bin/code")
	m, _ := queueFixture(t)
	m.editing = true
	if title := ansi.Strip(m.composerTitleLeft()); !strings.HasSuffix(title, "brouillon ouvert dans nvim") {
		t.Fatalf("EDITOR must win over VISUAL in the title, as it does for the launch, got %q", title)
	}
	if got := editorArgv("/tmp/x.md"); got[0] != "/usr/bin/nvim" {
		t.Fatalf("the launcher must run EDITOR first, got %v", got)
	}
}

func TestEditingPlaceholderNamesTheEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "/usr/bin/nvim")
	m, _ := queueFixture(t)
	m.editing = true
	want := "nvim est ouvert sur le brouillon, :wq pour revenir"
	if got := m.placeholderText(); got != want {
		t.Fatalf("the editing placeholder must name the editor, got %q", got)
	}
}

func TestDraftLineKeepsTheResumeHintRightAligned(t *testing.T) {
	m, _ := queueFixture(t)
	m.drafts = []string{"relis le plan"}
	line := ansi.Strip(m.draftsView())
	if !strings.HasPrefix(line, " ○ brouillons (1) : « relis le plan »") {
		t.Fatalf("the draft line must open with its count and preview, got %q", line)
	}
	if !strings.HasSuffix(line, "⌃S reprendre le dernier") {
		t.Fatalf("the draft line must end with the right-aligned resume hint, got %q", line)
	}
	if ansi.StringWidth(line) != m.innerWidth() {
		t.Fatalf("the draft line must span the composer width, got %d of %d", ansi.StringWidth(line), m.innerWidth())
	}
}
