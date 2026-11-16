package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestEnterWaitsWhenAnotherSessionIsActive(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}}}
	m := newModel(be)
	OpenOn("ghost", "")(m)
	m.Init()
	m.input.SetValue("hello")

	settle(m, m.handleEnter())

	if len(be.submitted) != 0 {
		t.Fatalf("submitted = %+v while the requested session is missing, want the text to wait", be.submitted)
	}
	if want := m.tr.t("tui.session.open_pending", map[string]any{tplArgName: "ghost"}); m.flash != want {
		t.Fatalf("flash = %q, want %q", m.flash, want)
	}
}

func TestLatestSubmitReplacesTheQueuedText(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()
	m.input.SetValue("bye")
	m.handleEnter()

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	settle(m, m.syncTabs())

	if len(be.submitted) != 1 || be.submitted[0].channel != "c2" || be.submitted[0].text != "bye" {
		t.Fatalf("submitted = %+v, want the latest text %q once on c2", be.submitted, "bye")
	}
}

func TestOneListReadCountsOneMiss(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}, revision: 1}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	for range 4 {
		m.syncTabs()
	}

	if m.openMisses != 1 {
		t.Fatalf("openMisses = %d after reading one list five times, want 1", m.openMisses)
	}
	if m.openSession != "later" {
		t.Fatalf("openSession = %q, want the request kept while one list was read", m.openSession)
	}
}

func TestOpenDoesNotStealFocusAfterTheUserLeftTheWaitingTab(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "main", ChannelID: "c1"},
		{Name: "two", ChannelID: "c2"},
	}, revision: 1}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	updateKey(m, tea.KeyMsg{Type: tea.KeyTab})

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c3"})
	be.revision++
	settle(m, m.syncTabs())

	if m.active != "c2" {
		t.Fatalf("active = %q after the requested session appeared, want the tab the user chose c2", m.active)
	}
}
