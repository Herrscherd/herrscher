package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestTabBeforeFirstTickSwitchesSession(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "main", ChannelID: "c1"},
		{Name: "two", ChannelID: "c2"},
	}}
	m := newModel(be)
	m.Init()

	updateKey(m, tea.KeyMsg{Type: tea.KeyTab})

	if m.active != "c2" {
		t.Fatalf("active = %q after Tab before the first tick, want c2", m.active)
	}
}

func TestEnterBeforeFirstTickSendsToTheRequestedSession(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "main", ChannelID: "c1"},
		{Name: "two", ChannelID: "c2"},
	}}
	m := newModel(be)
	OpenOn("two", "")(m)
	m.Init()
	m.input.SetValue("hello")

	settle(m, m.handleEnter())

	if m.starting {
		t.Fatal("Enter before the first tick started a new session instead of sending to the requested one")
	}
	if len(be.submitted) != 1 || be.submitted[0].channel != "c2" || be.submitted[0].text != "hello" {
		t.Fatalf("submitted = %+v, want one turn with %q on c2", be.submitted, "hello")
	}
}

func TestEnterWaitsWhileTheRequestedSessionIsMissing(t *testing.T) {
	be := &fakeBackend{}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")

	m.handleEnter()

	if m.starting {
		t.Fatal("Enter started a new session while the requested one was still missing")
	}
	if got := m.input.Value(); got != "hello" {
		t.Fatalf("composer = %q, want the text kept while waiting", got)
	}
}

func TestSessionAppearingInAReadyWindowShowsItsHistory(t *testing.T) {
	be := &fakeBackend{
		sessions:   []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}},
		scrollback: map[string][]contracts.ScrollbackLine{"main": {{Role: "user", Text: "remembered question"}}},
	}
	m := newModel(be)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.syncTabs()

	if !strings.Contains(m.vp.View(), "remembered question") {
		t.Fatalf("the transcript of the first session is blank in a window already sized:\n%s", m.vp.View())
	}
}
