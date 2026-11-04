package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestFrameHeightCountsBothSides(t *testing.T) {
	f := frame{above: []string{"rail"}, below: []string{"a", "b", "c"}}
	if got := f.height(); got != 4 {
		t.Fatalf("height = %d, want 4", got)
	}
}

func TestFrameRenderOrdersZonesAroundViewport(t *testing.T) {
	f := frame{above: []string{"rail"}, below: []string{"box", "status"}}
	want := "rail\nbody\nbox\nstatus"
	if got := f.render("body"); got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestFrameRenderKeepsMultiLineViewportIntact(t *testing.T) {
	f := frame{above: []string{"rail"}, below: []string{"status"}}
	want := "rail\none\ntwo\nstatus"
	if got := f.render("one\ntwo"); got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestFitBlockKeepsHeadAndFooter(t *testing.T) {
	lines := []string{"top", "a", "b", "c", "bottom"}
	if got := fitBlock(lines, 3); strings.Join(got, "|") != "top|a|bottom" {
		t.Fatalf("fitBlock = %q", got)
	}
	if got := fitBlock(lines, 0); got != nil {
		t.Fatalf("fitBlock without room = %q, want nothing", got)
	}
	if got := fitBlock(lines, 9); len(got) != len(lines) {
		t.Fatalf("fitBlock with room = %q, want every line", got)
	}
}

func TestFlexibleRowsNeverOverflowTheScreen(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	typeComposerLines(m, 10)
	queueItems(m, 3)
	assertFitsScreen(t, m.View(), 60, 16)
}

func typeComposerLines(m *model, lines int) {
	m.input.SetValue(strings.TrimSuffix(strings.Repeat("ligne\n", lines), "\n"))
	m.resizeComposer()
}

func queueItems(m *model, count int) {
	tb := m.tabs["c1"]
	for index := range count {
		tb.queue = append(tb.queue, queuedInput{text: "message " + strconv.Itoa(index+1)})
	}
	m.relayout()
}

func fitScenarios() map[string]func(t *testing.T, m *model) {
	return map[string]func(t *testing.T, m *model){
		"repos": func(_ *testing.T, _ *model) {},
		"approbation": func(t *testing.T, m *model) {
			t.Helper()
			m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c1"}, Event: approvalMenuEvent(t, "go test ./plugins/terminal/tui/", allApprovalRoles()...)})
		},
		"modes":    func(_ *testing.T, m *model) { m.openPlanMenu("c1") },
		"contexte": func(_ *testing.T, m *model) { m.openContext() },
		"barre":    func(_ *testing.T, m *model) { m.openStatusline() },
		"retour": func(_ *testing.T, m *model) {
			m.rwRows = []rewindRow{{Index: 1, At: "14:02", Text: "ajoute une file de messages"}, {Index: 2, At: "14:19", Text: "relance les tests"}}
			m.rwIdx = 1
			m.rwOpt = rewindOptionBoth
			m.rwStep = rewindStepList
			m.rwOpen = true
			m.relayout()
		},
		"tableau": func(_ *testing.T, m *model) { m.openBoard() },
		"composeur 10 lignes": func(_ *testing.T, m *model) {
			typeComposerLines(m, 10)
		},
		"composeur 25 lignes": func(_ *testing.T, m *model) {
			typeComposerLines(m, 25)
		},
		"file de 3": func(_ *testing.T, m *model) {
			queueItems(m, 3)
		},
		"palette": func(_ *testing.T, m *model) {
			m.input.SetValue("/")
			m.resizeComposer()
		},
		"aide": func(_ *testing.T, m *model) {
			m.showHelp = true
			m.relayout()
		},
		"chips": func(_ *testing.T, m *model) {
			m.pending = []Attachment{{Name: "capture.png", Mime: "image/png", Size: 2048}}
			m.relayout()
		},
		"combine": func(_ *testing.T, m *model) {
			queueItems(m, 3)
			typeComposerLines(m, 10)
			m.pending = []Attachment{{Name: "capture.png", Mime: "image/png", Size: 2048}}
			m.drafts = []string{"brouillon"}
			m.relayout()
		},
		"recherche":   func(_ *testing.T, m *model) { m.openSearch() },
		"sessions":    func(_ *testing.T, m *model) { m.openSwitch() },
		"reprise":     func(_ *testing.T, m *model) { m.openResume() },
		"compétences": func(_ *testing.T, m *model) { m.openSkills() },
		"diagnostic":  func(_ *testing.T, m *model) { m.openDiagnostics() },
		"plugins":     func(_ *testing.T, m *model) { _ = m.openPlugins() },
		"historique":  func(_ *testing.T, m *model) { m.openHistSearch() },
		"thème":       func(_ *testing.T, m *model) { m.openTheme() },
		"volets":      func(_ *testing.T, m *model) { m.openPanes() },
		"diff":        func(_ *testing.T, m *model) { _ = m.openDiff(false) },
		"mention": func(_ *testing.T, m *model) {
			m.input.SetValue("@")
			m.mentionCacheKey = m.worktreeDir() + "\x00"
			m.mentionCacheRows = []string{"internal/very/long/path/to/a/file/whose/name/keeps/going/on/and/on.go", "README.md"}
			m.resizeComposer()
		},
	}
}

func TestSurfacesFillTheScreenExactlyAtEverySize(t *testing.T) {
	sizes := []struct{ width, height int }{{60, 20}, {80, 20}, {120, 20}, {200, 20}, {60, 30}, {80, 30}, {120, 30}, {120, 42}, {200, 30}, {240, 50}}
	for name, open := range fitScenarios() {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%s %dx%d", name, size.width, size.height), func(t *testing.T) {
				m, _ := barModelAt(t, "")
				m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
				open(t, m)
				m.relayout()
				assertFitsScreen(t, m.View(), size.width, size.height)
			})
		}
	}
}

func TestEveryOverlayChangesTheMainView(t *testing.T) {
	base, _ := barModelAt(t, "")
	base.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	mainView := base.View()
	for name, open := range fitScenarios() {
		if name == "repos" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			m, _ := barModelAt(t, "")
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
			open(t, m)
			m.relayout()
			if m.View() == mainView {
				t.Fatalf("scenario %q renders the main view unchanged, the opener did not open its surface", name)
			}
		})
	}
}

func TestShortScreensNeverOverflow(t *testing.T) {
	for name, open := range fitScenarios() {
		for height := 10; height <= 16; height++ {
			t.Run(fmt.Sprintf("%s %dx%d", name, 80, height), func(t *testing.T) {
				m, _ := barModelAt(t, "")
				m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
				open(t, m)
				m.relayout()
				assertFitsScreen(t, m.View(), 80, height)
			})
		}
	}
}

func TestTinyScreensNeverPanic(t *testing.T) {
	for name, open := range fitScenarios() {
		for height := 1; height < 10; height++ {
			t.Run(fmt.Sprintf("%s %dx%d", name, 80, height), func(t *testing.T) {
				m, _ := barModelAt(t, "")
				m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
				open(t, m)
				m.relayout()
				m.View()
			})
		}
	}
}

func TestViewDoesNotChangeTheLayout(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	before := m.vp.Height
	m.showHelp = true
	m.View()
	if m.vp.Height != before {
		t.Fatalf("View changed the transcript height from %d to %d; only Update may resize", before, m.vp.Height)
	}
}

func TestViewDoesNotChangeTheComposer(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	placeholder := m.input.Placeholder
	text := m.input.FocusedStyle.Text.Render("probe")
	prompt := m.input.FocusedStyle.Prompt.Render("probe")
	m.activeTab().busy = true
	m.input.SetValue("!ls")
	m.View()
	if m.input.Placeholder != placeholder {
		t.Fatalf("View changed the placeholder from %q to %q", placeholder, m.input.Placeholder)
	}
	if m.input.FocusedStyle.Text.Render("probe") != text || m.input.FocusedStyle.Prompt.Render("probe") != prompt {
		t.Fatal("View changed the composer text or prompt style")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	if m.input.Placeholder != m.placeholderBusyText() {
		t.Fatalf("Update must sync the placeholder to the busy state, got %q", m.input.Placeholder)
	}
}

func assertFitsScreen(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Errorf("view has %d lines, the screen has %d", len(lines), height)
	}
	for index, line := range lines {
		if lipgloss.Width(line) > width {
			t.Errorf("line %d is %d cells wide, the screen is %d", index, lipgloss.Width(line), width)
		}
	}
}
