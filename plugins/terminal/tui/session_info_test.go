package tui

import (
	"testing"
	"time"

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

func TestActiveInfoIsReadOncePerFrame(t *testing.T) {
	be := &countingBackend{}
	be.sessions = []contracts.SessionInfo{{Name: "a", ChannelID: "c1", Vendor: "claude"}}
	m := newModel(be)
	m.ensureTab("c1")
	m.active = "c1"

	be.calls = 0
	for i := 0; i < 20; i++ {
		if s, ok := m.activeInfo(); !ok || s.Vendor != "claude" {
			t.Fatalf("activeInfo = %+v %v", s, ok)
		}
	}
	if be.calls != 1 {
		t.Fatalf("le hub a ete lu %d fois, une seule suffit", be.calls)
	}
}

func TestActiveInfoFollowsTheActiveTab(t *testing.T) {
	be := &countingBackend{}
	be.sessions = []contracts.SessionInfo{{Name: "a", ChannelID: "c1", Vendor: "claude"}, {Name: "b", ChannelID: "c2", Vendor: "codex"}}
	m := newModel(be)
	m.active = "c1"
	if s, _ := m.activeInfo(); s.Vendor != "claude" {
		t.Fatalf("vendor = %q", s.Vendor)
	}
	m.active = "c2"
	if s, _ := m.activeInfo(); s.Vendor != "codex" {
		t.Fatalf("changer d'onglet doit relire la fiche: %q", s.Vendor)
	}
}

func TestActiveInfoExpires(t *testing.T) {
	be := &countingBackend{}
	be.sessions = []contracts.SessionInfo{{Name: "a", ChannelID: "c1", Vendor: "claude"}}
	m := newModel(be)
	m.active = "c1"
	m.activeInfo()
	be.calls = 1
	m.infoAt = time.Now().Add(-2 * infoTTL)
	m.activeInfo()
	if be.calls != 2 {
		t.Fatalf("une fiche perimee doit etre relue: %d lectures", be.calls)
	}
}
