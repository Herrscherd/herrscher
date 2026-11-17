package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type probeBackend struct {
	*fakeBackend

	armed bool
	calls []string
}

func (b *probeBackend) note(call string) {
	if b.armed {
		b.calls = append(b.calls, call)
	}
}

func (b *probeBackend) Sessions() []contracts.SessionInfo {
	b.note("Sessions")
	return b.fakeBackend.Sessions()
}

func (b *probeBackend) Overview() (string, error) {
	b.note("Overview")
	return b.fakeBackend.Overview()
}

func (b *probeBackend) Scrollback(name string) []contracts.ScrollbackLine {
	b.note("Scrollback")
	return b.fakeBackend.Scrollback(name)
}

func (b *probeBackend) Dispatch(args []string) (string, error) {
	b.note("Dispatch")
	return b.fakeBackend.Dispatch(args)
}

func (b *probeBackend) Commands() []CommandSpec {
	b.note("Commands")
	return b.fakeBackend.Commands()
}

func (b *probeBackend) Seen(name string) {
	b.note("Seen")
	b.fakeBackend.Seen(name)
}

type viewSurface struct {
	name string
	open func(*model)
}

func TestViewNeverCallsTheDaemonOnAnySurface(t *testing.T) {
	surfaces := []viewSurface{
		{name: "chat", open: func(*model) {}},
		{name: "board", open: func(m *model) { m.openBoard() }},
		{name: "panes", open: func(m *model) { m.openPanes() }},
		{name: "context overlay", open: func(m *model) { m.openContext() }},
		{name: "resume overlay", open: func(m *model) { m.openResume() }},
		{name: "approval menu", open: func(m *model) {
			m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c1"}, Event: approvalMenuEvent(t, "go test ./...", allApprovalRoles()...)})
		}},
	}
	for _, surface := range surfaces {
		t.Run(surface.name, func(t *testing.T) {
			be := &probeBackend{fakeBackend: &fakeBackend{
				sessions: []contracts.SessionInfo{{Name: "alpha", ChannelID: "c1", Vendor: "claude", Gateways: []string{"terminal"}, Dir: "/tmp/alpha"}},
				overview: boardOverviewJSON,
			}}
			m := newModel(be)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
			syncLoaded(m)
			m.active = "c1"
			surface.open(m)

			be.armed = true
			m.View()
			be.armed = false
			if len(be.calls) > 0 {
				t.Fatalf("View on the %s surface called the daemon: %s", surface.name, strings.Join(be.calls, ", "))
			}
		})
	}
}

func TestWorktreeDirNeverReadsTheDaemon(t *testing.T) {
	be := &probeBackend{fakeBackend: &fakeBackend{
		sessions: []contracts.SessionInfo{{Name: "alpha", ChannelID: "c1", Dir: "/tmp/alpha"}},
	}}
	m := newModel(be)
	syncLoaded(m)
	m.active = "c1"

	be.armed = true
	dir := m.worktreeDir()
	be.armed = false
	if dir != "/tmp/alpha" {
		t.Fatalf("worktreeDir = %q, want the cached session dir", dir)
	}
	if len(be.calls) > 0 {
		t.Fatalf("worktreeDir called the daemon: %s", strings.Join(be.calls, ", "))
	}
}
