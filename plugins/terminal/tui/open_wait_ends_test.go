package tui

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestEmptyEnterEndsTheWaitSoALateArrivalNeitherActivatesNorStealsFocus(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	now := time.Unix(0, 0)
	m := startedModel(be)
	m.clock = func() time.Time { return now }
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()
	m.input.SetValue("")
	m.handleEnter()

	if m.awaitingOpen() {
		t.Fatal("the wait is still on after an empty Enter cancelled the queued text")
	}
	if want := m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: "later"}); m.flash != want {
		t.Fatalf("flash = %q after an empty Enter, want %q", m.flash, want)
	}

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	now = now.Add(openWaitLimit)
	syncLoaded(m)

	if m.active != "" {
		t.Fatalf("active = %q after the cancelled session arrived, want no activation", m.active)
	}
	if want := m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: "later"}); m.flash != want {
		t.Fatalf("flash = %q at the deadline, want the cancellation notice %q kept", m.flash, want)
	}
	if len(be.submitted) != 0 {
		t.Fatalf("submitted = %+v after the cancellation, want nothing sent", be.submitted)
	}
}

func TestRemoteCloseOfTheActiveTabKeepsTheQueuedTextInTheComposer(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "main", ChannelID: "c1"},
		{Name: "two", ChannelID: "c2"},
	}}
	m := startedModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()

	be.sessions = []contracts.SessionInfo{{Name: "two", ChannelID: "c2"}}
	syncLoaded(m)

	if m.awaitingOpen() {
		t.Fatal("the wait is still on after the daemon closed the active tab")
	}
	if got := m.input.Value(); got != "hello" {
		t.Fatalf("composer = %q after a remote close, want the queued text back in the composer", got)
	}
	if want := m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: "later"}); m.flash == want {
		t.Fatal("a remote close must not announce a cancellation the user never asked for")
	}
	if len(be.submitted) != 0 {
		t.Fatalf("submitted = %+v after a remote close, want nothing sent", be.submitted)
	}
}

func TestAfterTheLastTabClosesAnUnrequestedSessionTakesFocus(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}}}
	m := startedModel(be)
	m.Init()
	be.sessions = []contracts.SessionInfo{}
	syncLoaded(m)

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "other", ChannelID: "c2"})
	syncLoaded(m)

	if m.active != "c2" {
		t.Fatalf("active = %q after the last tab closed and an unrequested session appeared, want c2 focused as on master", m.active)
	}
}

func TestCommandLineAtArrivalSaysItWasKept(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := startedModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()
	m.input.SetValue("/help")

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	syncLoaded(m)

	if want := m.tr.t("tui.session.open_command_kept", map[string]any{tplArgName: "later"}); m.flash != want {
		t.Fatalf("flash = %q when a command line is kept at arrival, want %q", m.flash, want)
	}
}

func TestAttachmentAloneSaysItNeedsText(t *testing.T) {
	f := &fakeBackend{startName: "fresh", startChannel: "c9"}
	m := startedModel(f)
	m.pending = []Attachment{{Name: "a.png", Path: "/x/a.png"}}
	if cmd := m.handleEnter(); cmd != nil {
		t.Fatal("enter with an attachment alone must start nothing")
	}

	if want := m.tr.t("tui.session.attachment_needs_text", nil); m.flash != want {
		t.Fatalf("flash = %q for an attachment without text, want %q", m.flash, want)
	}
	if len(m.pending) != 1 {
		t.Fatalf("pending = %+v, want the attachment kept", m.pending)
	}
}
