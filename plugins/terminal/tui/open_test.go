package tui

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// A window opened on a task must land on that session even when it is not the
// first one the hub reports, and send the task there — that pairing is the whole
// of `herrscher "<task>"`.
func TestOpenOnFocusesSessionAndSendsTask(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "main", ChannelID: "c1"},
		{Name: "read-the-thread-u25m", ChannelID: "c2"},
	}}
	m := newModel(be)
	OpenOn("read-the-thread-u25m", "read the thread")(m)

	m.syncTabs()

	if m.active != "c2" {
		t.Fatalf("active = %q, want c2", m.active)
	}
	if len(be.submitted) != 1 {
		t.Fatalf("submitted %d turns, want 1", len(be.submitted))
	}
	if be.submitted[0].channel != "c2" || be.submitted[0].text != "read the thread" {
		t.Fatalf("submitted = %+v", be.submitted[0])
	}
	if tb := m.tabs["c2"]; tb == nil || !tb.busy {
		t.Fatal("the opened tab must show as working right away")
	}
}

// The task is sent once. syncTabs runs on every repaint, so a field left set
// would resend the opening message for as long as the window is open.
func TestOpenOnSendsOnlyOnce(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "s", ChannelID: "c1"}}}
	m := newModel(be)
	OpenOn("s", "go")(m)

	m.syncTabs()
	m.tabSig = "" // force a full reconcile, as a changed session list would
	m.syncTabs()

	if len(be.submitted) != 1 {
		t.Fatalf("submitted %d turns, want 1", len(be.submitted))
	}
}

// The session is usually not bridged yet when this process is the one starting
// the daemon. The window must wait for it rather than give up on the first
// reconcile — and must not fall back to sending the task into another session.
func TestOpenOnWaitsForTheSession(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}}}
	m := newModel(be)
	OpenOn("later", "go")(m)

	m.syncTabs()
	if len(be.submitted) != 0 {
		t.Fatalf("submitted %+v before the session existed", be.submitted)
	}

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	m.syncTabs()

	if m.active != "c2" || len(be.submitted) != 1 {
		t.Fatalf("active = %q, submitted = %+v", m.active, be.submitted)
	}
}

// OpenOn with no text is "show me this one": it selects the session without
// starting a turn.
func TestOpenOnWithoutTextSendsNothing(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "main", ChannelID: "c1"},
		{Name: "other", ChannelID: "c2"},
	}}
	m := newModel(be)
	OpenOn("other", "")(m)

	m.syncTabs()

	if m.active != "c2" {
		t.Fatalf("active = %q, want c2", m.active)
	}
	if len(be.submitted) != 0 {
		t.Fatalf("submitted %+v, want nothing", be.submitted)
	}
}
