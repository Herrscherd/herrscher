package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestTabDuringTheWaitCancelsTheQueueSoTheTextIsSentOnce(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "main", ChannelID: "c1"},
		{Name: "two", ChannelID: "c2"},
	}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()

	updateKey(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.awaitingOpen() || m.input.Value() != "hello" {
		t.Fatalf("awaiting = %v, composer = %q after the tab, want the wait ended and the text kept in the composer", m.awaitingOpen(), m.input.Value())
	}

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c3"})
	settle(m, m.syncTabs())
	if len(be.submitted) != 0 || m.active != "c2" {
		t.Fatalf("submitted = %+v, active = %q after the requested session appeared, want nothing sent and c2 kept", be.submitted, m.active)
	}

	settle(m, m.handleEnter())
	settle(m, m.syncTabs())
	if len(be.submitted) != 1 || be.submitted[0].channel != "c2" || be.submitted[0].text != "hello" {
		t.Fatalf("submitted = %+v, want hello sent exactly once to c2", be.submitted)
	}
}

func TestStartedSessionThatNeverGetsATabReleasesTheTextIntoTheComposer(t *testing.T) {
	be := &fakeBackend{startName: "fresh"}
	now := time.Unix(0, 0)
	m := newModel(be)
	m.clock = func() time.Time { return now }
	m.pending = []Attachment{{Name: "a.png", Path: "/x/a.png"}}
	m.input.SetValue("hello")
	started, ok := runCmd(m.handleEnter()).(sessionStartedMsg)
	if !ok {
		t.Fatal("enter with no session must start one")
	}
	settle(m, m.handleSessionStarted(started))
	m.input.SetValue(m.input.Value() + "\nsecond")

	now = now.Add(openWaitLimit)
	settle(m, m.syncTabs())

	if m.awaitingOpen() {
		t.Fatal("the wait is still on after the deadline with no tab for the started session")
	}
	if got := m.input.Value(); got != "hello\nsecond" {
		t.Fatalf("composer = %q after release, want the queued text and what was typed meanwhile", got)
	}
	if !strings.Contains(m.chipsView(), "a.png") {
		t.Fatalf("chips = %q after release, want the queued attachment still shown", m.chipsView())
	}
	if len(be.submitted) != 0 {
		t.Fatalf("submitted = %+v, want nothing sent", be.submitted)
	}
}

func TestEmptyEnterCancelsTheTextAndKeepsTheAttachmentsPending(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.pending = []Attachment{{Name: "a.png", Path: "/x/a.png"}}
	m.input.SetValue("hello")
	m.handleEnter()
	m.input.SetValue("")
	m.handleEnter()

	if want := m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: "later"}); m.flash != want {
		t.Fatalf("flash = %q after an empty Enter, want %q", m.flash, want)
	}
	if len(m.pending) != 1 || !strings.Contains(m.chipsView(), "a.png") {
		t.Fatalf("pending = %+v, chips = %q, want the attachment still pending", m.pending, m.chipsView())
	}
	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	settle(m, m.syncTabs())
	if len(be.submitted) != 0 {
		t.Fatalf("submitted = %+v after the text was cancelled, want nothing sent", be.submitted)
	}
}
