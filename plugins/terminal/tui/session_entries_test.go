package tui

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func assertShellRunsOnce(t *testing.T, f *fakeBackend, m *model) {
	t.Helper()
	typeLine(m, "!ls")
	deliver(m, m.handleEnter())
	if len(f.dispatched) != 1 {
		t.Fatalf("dispatched = %q, want the shell command sent once", f.dispatched)
	}
}

func TestOpenChannelRegistersAnAbsentSessionBeforeAnySessionsMsg(t *testing.T) {
	f := &fakeBackend{fe: make(chan RoutedEvent, 1)}
	m, _ := startModel(f)
	m.openChannel(contracts.SessionInfo{Name: "fresh", ChannelID: "c1"})
	assertShellRunsOnce(t, f, m)
}

func TestChooseSwitchRegistersAnAbsentSessionBeforeAnySessionsMsg(t *testing.T) {
	f := &fakeBackend{fe: make(chan RoutedEvent, 1)}
	m, _ := startModel(f)
	m.switchRows = []contracts.SessionInfo{{Name: "fresh", ChannelID: "c1"}}
	m.switchIdx = 0
	m.chooseSwitch()
	assertShellRunsOnce(t, f, m)
}

func TestOpenPanesRegistersAnAbsentSessionBeforeAnySessionsMsg(t *testing.T) {
	f := &fakeBackend{fe: make(chan RoutedEvent, 1), overview: boardOverviewJSON}
	m, _ := startModel(f)
	f.sessions = []contracts.SessionInfo{{Name: "alpha", ChannelID: "ch-alpha"}}
	m.openPanes()
	assertShellRunsOnce(t, f, m)
}

func TestRememberSessionKeepsTheNameAlreadyKnownForAChannel(t *testing.T) {
	f := &fakeBackend{fe: make(chan RoutedEvent, 1), sessions: []contracts.SessionInfo{{Name: "N1", ChannelID: "c1"}}}
	m := startedModel(f)
	m.ensureSessionTab(contracts.SessionInfo{ChannelID: "c1", Name: "N2"})
	if got := m.sessionName("c1"); got != "N1" {
		t.Fatalf("sessionName(c1) = %q, want the name already known for the channel", got)
	}
}

func TestAnUnnamedTabAttachedWithANameLoadsItsHistory(t *testing.T) {
	f := &fakeBackend{
		fe:         make(chan RoutedEvent, 1),
		scrollback: map[string][]contracts.ScrollbackLine{"s": {{Role: "user", Text: "old"}}},
	}
	m := startedModel(f)
	chat := m.ensureTab("c")
	m.ensureTabNamed("c", "s")
	loadHistory(m)
	if text := tabText(chat); !strings.Contains(text, "old") {
		t.Fatalf("tab text = %q, want the history of the session attached by name", text)
	}
}
