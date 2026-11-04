package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func contextModel(t *testing.T, used int) *model {
	t.Helper()
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude"}}}
	m := newModel(f)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	tb := m.ensureTab("c")
	m.active = "c"
	if used > 0 {
		tb.measureContext(contracts.Event{TokensIn: used})
	}
	m.openContext()
	return m
}

func TestContextOverlayWithoutDataShowsEveryCategoryAsUnavailable(t *testing.T) {
	m := contextModel(t, 0)
	view := ansi.Strip(m.contextView())
	for _, name := range []string{"système", "outils et MCP", "mémoire", "skills", "fichiers lus", "conversation"} {
		if !strings.Contains(view, name) {
			t.Fatalf("category %q missing from the overlay:\n%s", name, view)
		}
	}
	if strings.Count(view, unavailable) < len(contextCategories())+1 {
		t.Fatalf("every category and the total must read n/a without backend data:\n%s", view)
	}
	if strings.Contains(view, "%") && !strings.Contains(view, "n/a") {
		t.Fatalf("no percentage may be invented without data:\n%s", view)
	}
}

func TestContextOverlayWithDataShowsTheBackendFigures(t *testing.T) {
	m := contextModel(t, 82_000)
	view := ansi.Strip(m.contextView())
	for _, want := range []string{"82.0k/200k", "41%", "118.0k"} {
		if !strings.Contains(view, want) {
			t.Fatalf("overlay must show %q from the backend total:\n%s", want, view)
		}
	}
}

func TestContextOverlayCloseKeys(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyRunes, Runes: []rune{'z'}}} {
		m := contextModel(t, 0)
		m.Update(key)
		if m.ctxOpen {
			t.Fatalf("key %q must close the overlay", key.String())
		}
	}
}

func TestContextOverlayFitsWidths(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := contextModel(t, 82_000)
		m.handleWindowSize(tea.WindowSizeMsg{Width: width, Height: 24})
		for _, line := range strings.Split(m.contextView(), "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d: %d cells overflow: %q", width, lipgloss.Width(line), ansi.Strip(line))
			}
		}
	}
}

func TestContextOverlayIsAListUnderEightyColumns(t *testing.T) {
	narrow := contextModel(t, 82_000)
	narrow.handleWindowSize(tea.WindowSizeMsg{Width: 60, Height: 24})
	wide := contextModel(t, 82_000)
	wide.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 24})

	if strings.Contains(ansi.Strip(narrow.contextView()), "░") {
		t.Fatal("under 80 columns the categories must be a list without bars")
	}
	if !strings.Contains(ansi.Strip(wide.contextView()), "░") {
		t.Fatal("at 120 columns the categories must keep their bar column")
	}
}

func TestContextOverlayFitsTwentyRows(t *testing.T) {
	m := contextModel(t, 82_000)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 80, Height: 20})
	if rows := strings.Count(m.contextView(), "\n") + 1; rows > 20 {
		t.Fatalf("overlay takes %d rows, more than a 20-row window", rows)
	}
}

func TestContextViewShowsTheRealThresholdAndTheBackKey(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.openContext()
	view := ansi.Strip(m.contextView())
	if !strings.Contains(view, "  compacte seul à 92%") {
		t.Errorf("the total line must show the real compaction threshold after a gap:\n%s", view)
	}
	if !strings.Contains(view, "z revenir") {
		t.Errorf("the footer must name the z key:\n%s", view)
	}
}
