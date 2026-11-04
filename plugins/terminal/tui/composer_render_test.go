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
	if !strings.Contains(title, "! SHELL · dans …/.worktrees/tui-v2 · la sortie entre dans la session") {
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

func TestShellPromptStaysAngleBracketAndTextTurnsWarn(t *testing.T) {
	m, _ := queueFixture(t)
	typeLine(m, "!ls")
	m.buildFrame()
	if got := ansi.Strip(m.inputRow()); !strings.HasPrefix(got, "> !ls") {
		t.Fatalf("shell mode keeps the > prompt and shows the typed bang, got %q", got)
	}
	if m.input.FocusedStyle.Text.GetForeground() != m.st.warn.GetForeground() {
		t.Fatal("shell mode must colour the typed text with the warn style")
	}
	typeLine(m, "ls")
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
	return left
}
