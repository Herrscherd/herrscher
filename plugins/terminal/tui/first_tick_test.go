package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestTabBeforeFirstTickSwitchesSession(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "main", ChannelID: "c1"},
		{Name: "two", ChannelID: "c2"},
	}}
	m := startedModel(be)
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
	m := startedModel(be)
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
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := startedModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")

	m.handleEnter()

	if m.starting {
		t.Fatal("Enter started a new session while the requested one was still missing")
	}
	if want := m.tr.t("tui.session.open_pending", map[string]any{tplArgName: "later"}); m.flash != want {
		t.Fatalf("flash = %q while waiting, want %q", m.flash, want)
	}
	if got := m.input.Value(); got != "hello" {
		t.Fatalf("composer = %q, want the text kept while waiting", got)
	}
}

func TestSlashCommandsStillRunWhileTheRequestedSessionIsMissing(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := startedModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("/bogus")

	settle(m, m.handleEnter())

	if len(be.dispatched) != 1 || be.dispatched[0][0] != "bogus" {
		t.Fatalf("dispatched = %+v, want the slash command to run", be.dispatched)
	}
}

func TestQueuedTextIsSentOnceTheSessionAppears(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := startedModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	syncLoaded(m)

	if len(be.submitted) != 1 || be.submitted[0].channel != "c2" || be.submitted[0].text != "hello" {
		t.Fatalf("submitted = %+v, want %q once on c2", be.submitted, "hello")
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("composer = %q after the send, want it cleared", got)
	}
}

func TestMissingSessionIsReleasedWithNoticeAndTextKept(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	now := time.Unix(0, 0)
	m := startedModel(be)
	m.clock = func() time.Time { return now }
	OpenOn("ghost", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()

	syncLoaded(m)
	now = now.Add(openWaitLimit)
	syncLoaded(m)

	if m.open.session != "" {
		t.Fatalf("open.session = %q, want it released once the deadline passed with the session still missing", m.open.session)
	}
	if want := m.tr.t("tui.session.open_missing", map[string]any{tplArgName: "ghost"}); m.flash != want {
		t.Fatalf("flash = %q after release, want %q", m.flash, want)
	}
	if got := m.input.Value(); got != "hello" {
		t.Fatalf("composer = %q after release, want the text kept", got)
	}
}

func TestSessionAppearingInAReadyWindowShowsItsHistory(t *testing.T) {
	be := &fakeBackend{
		sessions:   []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}},
		scrollback: map[string][]contracts.ScrollbackLine{"main": {{Role: "user", Text: "remembered question"}}},
	}
	m := startedModel(be)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 40})

	syncLoaded(m)

	if !strings.Contains(m.vp.View(), "remembered question") {
		t.Fatalf("the transcript of the first session is blank in a window already sized:\n%s", m.vp.View())
	}
}
