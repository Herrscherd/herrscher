package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type silentBackend struct {
	*fakeBackend

	release chan struct{}
	mute    bool
}

func (b *silentBackend) Sessions() []contracts.SessionInfo {
	if !b.mute {
		return b.fakeBackend.Sessions()
	}
	<-b.release
	return nil
}

func returnsWithin(t *testing.T, what string, run func()) {
	t.Helper()
	returned := make(chan struct{})
	go func() {
		run()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal(what)
	}
}

func TestMuteDaemonDoesNotBlockTheTick(t *testing.T) {
	be := &silentBackend{fakeBackend: &fakeBackend{}, release: make(chan struct{}), mute: true}
	defer close(be.release)
	m := newModel(be)

	returnsWithin(t, "a tick with a silent daemon blocked the UI thread, the session list must be read off the update loop", func() {
		m.Update(tickMsg{})
	})
}

func TestViewNeverReadsTheDaemonOnTheUpdateLoop(t *testing.T) {
	be := &silentBackend{
		fakeBackend: &fakeBackend{sessions: []contracts.SessionInfo{{Name: "a", ChannelID: "c1", Vendor: "claude"}}},
		release:     make(chan struct{}),
	}
	defer close(be.release)
	m := newModel(be)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.syncTabs()

	be.mute = true
	returnsWithin(t, "View blocked on a silent daemon, the status line must render from the cache", func() {
		m.View()
	})
}

func TestAStaleSnapshotLandingLateDoesNotReopenAClosedTab(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}, {Name: "two", ChannelID: "c2"}}}
	m := newModel(be)
	m.Init()
	stale := runCmd(m.refreshTabs())

	be.sessions = []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}}
	settle(m, m.syncTabs())
	if m.tabs["c2"] != nil {
		t.Fatal("the synchronous read must close the tab that the daemon no longer lists")
	}

	m.Update(stale)
	if m.tabs["c2"] != nil {
		t.Fatal("a snapshot read before the tab closed reopened it when it landed late")
	}
}

func TestOnlyOneSessionReadIsInFlight(t *testing.T) {
	m := newModel(&fakeBackend{})
	first := m.refreshTabs()
	if first == nil {
		t.Fatal("the first read must start")
	}
	if second := m.refreshTabs(); second != nil {
		t.Fatal("a second read started while the first was still in flight")
	}

	m.Update(runCmd(first))
	if m.refreshTabs() == nil {
		t.Fatal("the landed read must free the next one")
	}
}

func TestApplyingASnapshotDecidesFocusWithoutReadingTheDaemon(t *testing.T) {
	be := &silentBackend{fakeBackend: &fakeBackend{}, release: make(chan struct{}), mute: true}
	defer close(be.release)
	m := newModel(be)
	m.heldSession = "later"

	returnsWithin(t, "applying a snapshot read the daemon again to decide focus", func() {
		m.Update(sessionsMsg{seq: 1, infos: []contracts.SessionInfo{{Name: "later", ChannelID: "c2"}}})
	})
	if m.tabs["c2"] == nil {
		t.Fatal("the held session's tab must still be listed")
	}
	if m.active != "" {
		t.Fatalf("active = %q, the held session must not take focus on arrival", m.active)
	}
}

func TestAnExplicitOpenReleasesTheHeldSession(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "later", ChannelID: "c2"}}}
	m := newModel(be)
	m.heldSession = "later"

	m.openChannel(contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	if m.heldSession != "" {
		t.Fatalf("heldSession = %q after the user opened that session, want it released", m.heldSession)
	}
	if m.active != "c2" {
		t.Fatalf("active = %q after the user opened the session, want c2", m.active)
	}
}
