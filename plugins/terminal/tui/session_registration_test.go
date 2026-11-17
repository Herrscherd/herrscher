package tui

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestKeyOnAForkedSessionReachesTheDaemonBeforeAnySessionsMsg(t *testing.T) {
	f := &fakeBackend{fe: make(chan RoutedEvent, 1)}
	m := startedModel(f)
	m.Update(forkResolvedMsg{info: contracts.SessionInfo{Name: "forked", ChannelID: "cf"}})
	typeLine(m, "!ls")
	deliver(m, m.handleEnter())
	if len(f.dispatched) != 1 {
		t.Fatalf("dispatched = %q, want the shell command sent once", f.dispatched)
	}
}

func TestKeyOnAnUnknownSessionKeepsTheTextAndSaysWhy(t *testing.T) {
	f := &fakeBackend{fe: make(chan RoutedEvent, 1)}
	m := startedModel(f)
	m.ensureTab("orphan")
	m.active = "orphan"
	typeLine(m, "!ls")
	deliver(m, m.handleEnter())
	if len(f.dispatched) != 0 {
		t.Fatalf("dispatched = %q, want nothing sent for an unknown session", f.dispatched)
	}
	if got := m.input.Value(); got != "!ls" {
		t.Errorf("composer = %q, want the text kept", got)
	}
	if want := m.tr.t("tui.shell.no_session", nil); m.flash != want {
		t.Errorf("flash = %q, want %q", m.flash, want)
	}
}
