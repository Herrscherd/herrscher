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
	syncLoaded(m)
	if len(be.submitted) != 0 || m.active != "c2" {
		t.Fatalf("submitted = %+v, active = %q after the requested session appeared, want nothing sent and c2 kept", be.submitted, m.active)
	}

	settle(m, m.handleEnter())
	syncLoaded(m)
	if len(be.submitted) != 1 || be.submitted[0].channel != "c2" || be.submitted[0].text != "hello" {
		t.Fatalf("submitted = %+v, want hello sent exactly once to c2", be.submitted)
	}
}

func TestEditedComposerDuringTheWaitIsWhatGoesOut(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()
	m.input.SetValue("hello!")

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	syncLoaded(m)

	if len(be.submitted) != 1 || be.submitted[0].text != "hello!" || m.input.Value() != "" {
		t.Fatalf("submitted = %+v, composer after arrival = %q, want the edited text %q sent once and the composer cleared", be.submitted, m.input.Value(), "hello!")
	}
}

func TestEmptiedComposerDuringTheWaitSendsNothing(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()
	m.input.SetValue("")

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	syncLoaded(m)

	if len(be.submitted) != 0 {
		t.Fatalf("submitted = %+v after the user emptied the composer, want nothing sent", be.submitted)
	}
	if want := m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: "later"}); m.flash != want {
		t.Fatalf("flash = %q at arrival of an emptied composer, want %q", m.flash, want)
	}
}

func TestCommandLineInTheComposerAtArrivalIsNotSent(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()
	m.input.SetValue("/help")

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	syncLoaded(m)

	if len(be.submitted) != 0 || m.input.Value() != "/help" {
		t.Fatalf("submitted = %+v, composer = %q, want the command line left in the composer and nothing sent", be.submitted, m.input.Value())
	}
}

func TestStartedSessionThatNeverGetsATabReleasesTheTextIntoTheComposer(t *testing.T) {
	be := &fakeBackend{startName: "fresh", startChannel: "c9"}
	now := time.Unix(0, 0)
	m := newModel(be)
	m.clock = func() time.Time { return now }
	m.pending = []Attachment{{Name: "a.png", Path: "/x/a.png"}}
	m.input.SetValue("hello")
	started, ok := runCmd(m.handleEnter()).(sessionStartedMsg)
	if !ok {
		t.Fatal("enter with no session must start one")
	}
	be.sessions[len(be.sessions)-1].Archived = true
	settle(m, m.handleSessionStarted(started))
	m.input.SetValue(m.input.Value() + "\nsecond")

	now = now.Add(openWaitLimit)
	syncLoaded(m)

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

func TestTabChangeDuringTheStartKeepsTheTextForTheStartedSession(t *testing.T) {
	f := &fakeBackend{startName: "fresh", startChannel: "c9", sessions: []contracts.SessionInfo{{Name: "two", ChannelID: "c2"}}}
	m := newModel(f)
	m.Init()
	m.active = ""
	m.input.SetValue("hello")
	started, ok := runCmd(m.handleEnter()).(sessionStartedMsg)
	if !ok {
		t.Fatal("enter with no session must start one")
	}
	m.activate("c2")
	settle(m, m.handleSessionStarted(started))
	syncLoaded(m)

	if len(f.submitted) != 1 || f.submitted[0].channel != "c9" || f.submitted[0].text != "hello" {
		t.Fatalf("submitted = %+v, want hello once on c9 after a tab change during the start", f.submitted)
	}
}

func TestAttachmentsNeverStartASessionOrGoAloneWithoutText(t *testing.T) {
	f := &fakeBackend{startName: "fresh", startChannel: "c9"}
	m := newModel(f)
	m.pending = []Attachment{{Name: "a.png", Path: "/x/a.png"}}
	if cmd := m.handleEnter(); cmd != nil {
		t.Fatal("enter with no session, an empty composer and an attachment must do nothing")
	}
	syncLoaded(m)

	if len(f.sessions) != 0 || len(f.submitted) != 0 {
		t.Fatalf("sessions = %+v, submitted = %+v, want nothing started or sent", f.sessions, f.submitted)
	}
	if len(m.pending) != 1 {
		t.Fatalf("pending = %+v, want the attachment kept", m.pending)
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
	syncLoaded(m)
	if len(be.submitted) != 0 {
		t.Fatalf("submitted = %+v after the text was cancelled, want nothing sent", be.submitted)
	}
}

func TestRemovingTheOnlyActiveTabEndsTheWait(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	be.sessions = []contracts.SessionInfo{}
	syncLoaded(m)
	if m.awaitingOpen() {
		t.Fatal("the wait is still on after the active tab was removed with no successor")
	}
	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c3"})
	syncLoaded(m)
	if m.active == "c3" {
		t.Fatal("the awaited session took focus after the user's active tab was removed")
	}
}

func TestCancelledWaitKeepsItsNoticeAtTheDeadline(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "main", ChannelID: "c1"},
		{Name: "two", ChannelID: "c2"},
	}}
	now := time.Unix(0, 0)
	m := newModel(be)
	m.clock = func() time.Time { return now }
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()
	m.activate("c2")

	now = now.Add(openWaitLimit)
	syncLoaded(m)

	if want := m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: "later"}); m.flash != want {
		t.Fatalf("flash = %q at the deadline of a cancelled wait, want the cancellation notice %q kept", m.flash, want)
	}
}
