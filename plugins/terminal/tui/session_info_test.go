package tui

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type countingBackend struct {
	fakeBackend

	calls int
}

func (b *countingBackend) Sessions() []contracts.SessionInfo {
	b.calls++
	return b.sessions
}

func TestActiveInfoNeverReadsTheDaemon(t *testing.T) {
	be := &countingBackend{}
	be.sessions = []contracts.SessionInfo{{Name: "a", ChannelID: "c1", Vendor: "claude"}}
	m := startedModel(be)
	syncLoaded(m)

	be.calls = 0
	for range 20 {
		if s, ok := m.activeInfo(); !ok || s.Vendor != "claude" {
			t.Fatalf("activeInfo = %+v %v", s, ok)
		}
	}
	if be.calls != 0 {
		t.Fatalf("activeInfo read the daemon %d times on the render path, want 0", be.calls)
	}
}

func TestActiveInfoFollowsTheActiveTab(t *testing.T) {
	be := &countingBackend{}
	be.sessions = []contracts.SessionInfo{{Name: "a", ChannelID: "c1", Vendor: "claude"}, {Name: "b", ChannelID: "c2", Vendor: "codex"}}
	m := startedModel(be)
	syncLoaded(m)
	m.active = "c1"
	if s, _ := m.activeInfo(); s.Vendor != "claude" {
		t.Fatalf("vendor = %q", s.Vendor)
	}
	m.active = "c2"
	if s, _ := m.activeInfo(); s.Vendor != "codex" {
		t.Fatalf("changer d'onglet doit suivre la fiche déjà lue: %q", s.Vendor)
	}
}
