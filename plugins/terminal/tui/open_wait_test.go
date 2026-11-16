package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestEnterWaitsWhenAnotherSessionIsActive(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}}}
	m := newModel(be)
	OpenOn("ghost", "")(m)
	m.Init()
	m.input.SetValue("hello")

	settle(m, m.handleEnter())

	if len(be.submitted) != 0 {
		t.Fatalf("submitted = %+v while the requested session is missing, want the text to wait", be.submitted)
	}
	if want := m.tr.t("tui.session.open_pending", map[string]any{tplArgName: "ghost"}); m.flash != want {
		t.Fatalf("flash = %q, want %q", m.flash, want)
	}
}

func TestLatestSubmitReplacesTheQueuedText(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()
	m.input.SetValue("bye")
	m.handleEnter()

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	settle(m, m.syncTabs())

	if len(be.submitted) != 1 || be.submitted[0].channel != "c2" || be.submitted[0].text != "bye" {
		t.Fatalf("submitted = %+v, want the latest text %q once on c2", be.submitted, "bye")
	}
}

func TestBurstOfListsDoesNotReleaseBeforeTheDeadline(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	now := time.Unix(0, 0)
	m := newModel(be)
	m.clock = func() time.Time { return now }
	OpenOn("later", "")(m)
	m.Init()
	for range 10 {
		m.syncTabs()
	}

	if m.open.session != "later" {
		t.Fatalf("open.session = %q after a burst of lists at one instant, want the request kept", m.open.session)
	}
	now = now.Add(openWaitLimit - time.Nanosecond)
	m.syncTabs()
	if m.open.session != "later" {
		t.Fatalf("open.session = %q one nanosecond before the deadline, want the request kept", m.open.session)
	}
	now = now.Add(time.Nanosecond)
	m.syncTabs()
	if m.open.session != "" {
		t.Fatalf("open.session = %q at the deadline, want it released", m.open.session)
	}
}

func TestFrozenListIsReleasedAtTheDeadline(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "main", ChannelID: "c1"}}}
	now := time.Unix(0, 0)
	m := newModel(be)
	m.clock = func() time.Time { return now }
	OpenOn("ghost", "")(m)
	m.Init()
	for elapsed := time.Second; elapsed < openWaitLimit; elapsed += time.Second {
		now = now.Add(time.Second)
		m.syncTabs()
		if m.open.session == "" {
			t.Fatalf("released after %v, want the request kept until %v", elapsed, openWaitLimit)
		}
	}
	now = now.Add(time.Second)
	m.syncTabs()
	if m.open.session != "" {
		t.Fatalf("open.session = %q at the deadline with the same list, want it released", m.open.session)
	}
}

func TestOpenDoesNotStealFocusAfterTheUserLeftTheWaitingTab(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "main", ChannelID: "c1"},
		{Name: "two", ChannelID: "c2"},
	}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	updateKey(m, tea.KeyMsg{Type: tea.KeyTab})

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c3"})
	settle(m, m.syncTabs())

	if m.active != "c2" {
		t.Fatalf("active = %q after the requested session appeared, want the tab the user chose c2", m.active)
	}
}

func TestUserChoiceAtEachEntryPointEndsTheWait(t *testing.T) {
	entries := []struct {
		name   string
		choose func(m *model)
	}{
		{"resume picker", func(m *model) {
			m.openChannel(contracts.SessionInfo{Name: "two", ChannelID: "c2"})
		}},
		{"switch picker", func(m *model) {
			m.switchRows = []contracts.SessionInfo{{Name: "two", ChannelID: "c2"}}
			m.switchIdx = 0
			m.chooseSwitch()
		}},
		{"fork opened", func(m *model) {
			m.handleForkDone(forkDoneMsg{name: "two"})
		}},
	}
	for _, entry := range entries {
		t.Run(entry.name, func(t *testing.T) {
			be := &fakeBackend{sessions: []contracts.SessionInfo{
				{Name: "main", ChannelID: "c1"},
				{Name: "two", ChannelID: "c2"},
			}}
			m := newModel(be)
			OpenOn("ghost", "")(m)
			m.Init()

			entry.choose(m)
			if m.active != "c2" || m.awaitingOpen() {
				t.Fatalf("active = %q, awaiting = %v after the choice, want c2 and no wait", m.active, m.awaitingOpen())
			}

			m.input.SetValue("hello")
			settle(m, m.handleEnter())
			if len(be.submitted) != 1 || be.submitted[0].channel != "c2" || be.submitted[0].text != "hello" {
				t.Fatalf("submitted = %+v, want hello sent at once to the displayed c2", be.submitted)
			}

			be.sessions = append(be.sessions, contracts.SessionInfo{Name: "ghost", ChannelID: "c3"})
			settle(m, m.syncTabs())
			if m.active != "c2" || len(be.submitted) != 1 {
				t.Fatalf("active = %q, submitted = %+v after ghost appeared, want c2 kept and nothing resent", m.active, be.submitted)
			}
		})
	}
}

func TestEmptyComposerCancelsTheQueuedText(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.input.SetValue("hello")
	m.handleEnter()
	m.input.SetValue("")
	m.handleEnter()

	if want := m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: "later"}); m.flash != want {
		t.Fatalf("flash = %q after an empty Enter, want %q", m.flash, want)
	}
	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	settle(m, m.syncTabs())
	if len(be.submitted) != 0 {
		t.Fatalf("submitted = %+v after the queue was cancelled, want nothing sent", be.submitted)
	}
}

func TestQueuedAttachmentStaysVisibleAndIsSentOnce(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.pending = []Attachment{{Name: "a.png", Path: "/x/a.png"}}
	m.input.SetValue("hello")
	m.handleEnter()

	if !strings.Contains(m.chipsView(), "a.png") {
		t.Fatalf("chips = %q while queued, want the queued attachment still shown", m.chipsView())
	}
	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	settle(m, m.syncTabs())
	settle(m, m.syncTabs())

	if len(be.submitted) != 1 || be.submitted[0].text != "hello" || len(be.submitted[0].atts) != 1 {
		t.Fatalf("submitted = %+v, want hello with one attachment sent exactly once", be.submitted)
	}
	if len(m.pending) != 0 {
		t.Fatalf("pending = %+v after delivery, want none left to resend", m.pending)
	}
}

func TestQueuedAttachmentCanBeRemovedBeforeDelivery(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	m.pending = []Attachment{{Name: "a.png", Path: "/x/a.png"}, {Name: "b.png", Path: "/x/b.png"}}
	m.input.SetValue("hello")
	m.handleEnter()

	if !m.removeLastPending() {
		t.Fatal("removing a queued attachment must report true")
	}
	if strings.Contains(m.chipsView(), "b.png") || !strings.Contains(m.chipsView(), "a.png") {
		t.Fatalf("chips = %q after removing b.png, want only a.png", m.chipsView())
	}
	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c2"})
	settle(m, m.syncTabs())

	if len(be.submitted) != 1 || len(be.submitted[0].atts) != 1 || be.submitted[0].atts[0].Name != "a.png" {
		t.Fatalf("submitted = %+v, want only the attachment that stayed", be.submitted)
	}
}

func TestActiveTabRemovedDuringTheWaitDoesNotLetTheTargetTakeFocus(t *testing.T) {
	be := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "main", ChannelID: "c1"},
		{Name: "two", ChannelID: "c2"},
	}}
	m := newModel(be)
	OpenOn("later", "")(m)
	m.Init()
	if m.active != "c1" {
		t.Fatalf("active = %q before the removal, want c1", m.active)
	}

	be.sessions = []contracts.SessionInfo{{Name: "two", ChannelID: "c2"}}
	settle(m, m.syncTabs())
	if m.active != "c2" {
		t.Fatalf("active = %q after its tab went away, want the successor c2", m.active)
	}

	be.sessions = append(be.sessions, contracts.SessionInfo{Name: "later", ChannelID: "c3"})
	settle(m, m.syncTabs())
	if m.active != "c2" {
		t.Fatalf("active = %q after the requested session appeared, want c2 kept", m.active)
	}
}

func TestOpenWaitReleasesWhenNoListEverArrives(t *testing.T) {
	be := &fakeBackend{}
	now := time.Unix(0, 0)
	m := newModel(be)
	m.clock = func() time.Time { return now }
	OpenOn("ghost", "")(m)
	m.Init()

	now = now.Add(openWaitLimit)
	m.syncTabs()

	if m.open.session != "" {
		t.Fatalf("open.session = %q with no session list ever read, want it released at the deadline", m.open.session)
	}
	if want := m.tr.t("tui.session.open_missing", map[string]any{tplArgName: "ghost"}); m.flash != want {
		t.Fatalf("flash = %q, want %q", m.flash, want)
	}
}

func TestStartingSessionKeepsItsAttachmentsVisibleAndSentOnce(t *testing.T) {
	f := &fakeBackend{startName: "fresh", startChannel: "c9"}
	m := newModel(f)
	m.pending = []Attachment{{Name: "a.png", Path: "/x/a.png"}}
	m.input.SetValue("hello")
	started, ok := runCmd(m.handleEnter()).(sessionStartedMsg)
	if !ok {
		t.Fatal("enter with no session must start one")
	}

	if !strings.Contains(m.chipsView(), "a.png") {
		t.Fatalf("chips = %q while the session starts, want the attachment still shown", m.chipsView())
	}
	settle(m, m.handleSessionStarted(started))
	settle(m, m.syncTabs())

	if len(f.submitted) != 1 || f.submitted[0].channel != "c9" || f.submitted[0].text != "hello" || len(f.submitted[0].atts) != 1 {
		t.Fatalf("submitted = %+v, want hello with the attachment sent once to c9", f.submitted)
	}
	if len(m.pending) != 0 {
		t.Fatalf("pending = %+v after delivery, want none left to resend", m.pending)
	}
}
