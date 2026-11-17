package tui

import (
	"errors"
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

func (b *silentBackend) Overview() (string, error) {
	if !b.mute {
		return b.fakeBackend.Overview()
	}
	<-b.release
	return "", nil
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
	be := &silentBackend{fakeBackend: &fakeBackend{}, release: make(chan struct{}), mute: false}
	defer close(be.release)
	m := startedModel(be)
	be.mute = true

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
	m := startedModel(be)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	syncLoaded(m)

	be.mute = true
	returnsWithin(t, "View blocked on a silent daemon, the status line must render from the cache", func() {
		m.View()
	})
}

func TestAStaleSnapshotLandingLateDoesNotReopenAClosedTab(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}, {Name: "two", ChannelID: "c2"}}}
	m := startedModel(be)
	m.Init()
	stale := runCmd(m.refreshTabs())

	be.sessions = []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}}
	syncLoaded(m)
	if m.tabs["c2"] != nil {
		t.Fatal("the synchronous read must close the tab that the daemon no longer lists")
	}

	m.Update(stale)
	if m.tabs["c2"] != nil {
		t.Fatal("a snapshot read before the tab closed reopened it when it landed late")
	}
}

func TestOnlyOneSessionReadIsInFlight(t *testing.T) {
	m := startedModel(&fakeBackend{})
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
	be := &silentBackend{fakeBackend: &fakeBackend{}, release: make(chan struct{}), mute: false}
	defer close(be.release)
	m := startedModel(be)
	be.mute = true
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
	m := startedModel(be)
	m.heldSession = "later"

	m.openChannel(contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	if m.heldSession != "" {
		t.Fatalf("heldSession = %q after the user opened that session, want it released", m.heldSession)
	}
	if m.active != "c2" {
		t.Fatalf("active = %q after the user opened the session, want c2", m.active)
	}
}

func TestMuteDaemonDoesNotBlockTheTickOnTheBoard(t *testing.T) {
	be := &silentBackend{fakeBackend: &fakeBackend{}, release: make(chan struct{}), mute: false}
	defer close(be.release)
	m := startedModel(be)
	be.mute = true
	m.surface = surfaceBoard

	returnsWithin(t, "a tick with a silent daemon blocked the board view on the update loop, the overview and the session list must be read off the loop", func() {
		m.Update(tickMsg{})
	})
}

func TestMuteDaemonDoesNotBlockTheTickOnThePanes(t *testing.T) {
	be := &silentBackend{fakeBackend: &fakeBackend{}, release: make(chan struct{}), mute: false}
	defer close(be.release)
	m := startedModel(be)
	be.mute = true
	m.surface = surfacePanes

	returnsWithin(t, "a tick with a silent daemon blocked the panes view on the update loop, the overview and the session list must be read off the loop", func() {
		m.Update(tickMsg{})
	})
}

func TestBoardRowsReadTheSnapshotNotTheDaemon(t *testing.T) {
	be := &silentBackend{fakeBackend: &fakeBackend{}, release: make(chan struct{}), mute: false}
	defer close(be.release)
	m := startedModel(be)
	be.mute = true
	m.overviews = []SessionOverview{{Name: "a"}}
	m.boardInfos = []contracts.SessionInfo{{Name: "a", ChannelID: "c1"}}

	var rows []boardRow
	returnsWithin(t, "building the board rows read the daemon instead of the snapshot", func() {
		rows = m.boardCandidates()
	})
	if len(rows) != 1 {
		t.Fatalf("board rows = %d, want the one session of the snapshot", len(rows))
	}
}

func TestOnlyOneSurfaceReadIsInFlight(t *testing.T) {
	m := startedModel(&fakeBackend{})
	m.surface = surfaceBoard
	first := m.refreshSurfaceCmd()
	if first == nil {
		t.Fatal("the first surface read must start")
	}
	if second := m.refreshSurfaceCmd(); second != nil {
		t.Fatal("a second surface read started while the first was still in flight")
	}

	m.Update(runCmd(first))
	if m.refreshSurfaceCmd() == nil {
		t.Fatal("the landed surface read must free the next one")
	}
}

func TestASurfaceReadErrorStillFreesTheNextRead(t *testing.T) {
	m := startedModel(&fakeBackend{overviewErr: errors.New("daemon down")})
	m.surface = surfaceBoard
	first := m.refreshSurfaceCmd()
	m.Update(runCmd(first))

	if m.refreshSurfaceCmd() == nil {
		t.Fatal("a failed surface read must free the next one")
	}
	if m.flash == "" {
		t.Fatal("a failed overview read must say so in the flash")
	}
}

func TestAStaleSurfaceSnapshotIsIgnored(t *testing.T) {
	m := startedModel(&fakeBackend{})
	m.surface = surfaceBoard
	m.Update(surfaceMsg{seq: 2, overviews: []SessionOverview{{Name: "new"}}})
	m.Update(surfaceMsg{seq: 1, overviews: []SessionOverview{{Name: "old"}}})

	if len(m.overviews) != 1 || m.overviews[0].Name != "new" {
		t.Fatalf("overviews = %+v, a snapshot read before the last applied one must be ignored", m.overviews)
	}
}
