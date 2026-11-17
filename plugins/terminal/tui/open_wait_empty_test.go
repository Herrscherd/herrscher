package tui

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestEmptyEnterEndsAWaitThatHoldsNoText(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	now := time.Unix(0, 0)
	m := newModel(be)
	m.clock = func() time.Time { return now }
	OpenOn("later", "")(m)
	m.Init()
	m.handleEnter()

	if m.awaitingOpen() {
		t.Fatal("the wait is still on after an empty Enter with no text queued")
	}
	if want := m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: "later"}); m.flash != want {
		t.Fatalf("flash = %q after an empty Enter, want the cancellation notice %q", m.flash, want)
	}

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	now = now.Add(openWaitLimit)
	syncLoaded(m)

	if m.active != "" {
		t.Fatalf("active = %q when the cancelled session arrived, want no activation", m.active)
	}
}

func TestEmptyTextWithoutAWaitKeepsTheHeldSession(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.heldSession = "later"
	flash := m.flash

	m.queueForOpen("")

	if m.heldSession != "later" {
		t.Fatalf("heldSession = %q after an empty Enter with no wait, want the held session kept", m.heldSession)
	}
	if m.flash != flash {
		t.Fatalf("flash = %q after an empty Enter with no wait, want it unchanged", m.flash)
	}
}
