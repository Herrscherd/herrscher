package tui

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func newTestModel() *model { return newModel(nil) }

func TestRoutedEventLandsInOwnTab(t *testing.T) {
	m := newTestModel()
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "chunk", Text: "hello-a"}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "b"}, Event: contracts.Event{T: "chunk", Text: "hello-b"}})

	if got := strings.Join(m.tabs["a"].lines, "\n"); !strings.Contains(got, "hello-a") {
		t.Fatalf("tab a missing its line: %q", got)
	}
	if got := strings.Join(m.tabs["b"].lines, "\n"); strings.Contains(got, "hello-a") {
		t.Fatalf("tab b leaked tab a's line: %q", got)
	}
}

func TestUnreadSetOnInactiveTab(t *testing.T) {
	m := newTestModel()
	m.ensureTab("a") // first tab becomes active
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "b"}, Event: contracts.Event{T: "chunk", Text: "x"}})
	if !m.tabs["b"].unread {
		t.Fatal("event on inactive tab b must mark it unread")
	}
	if m.tabs["a"].unread {
		t.Fatal("active tab a must not be unread")
	}
}

func TestSwitchTabClearsUnread(t *testing.T) {
	m := newTestModel()
	m.ensureTab("a")
	m.ensureTab("b")
	m.tabs["b"].unread = true
	m.active = "a"
	m.switchTab(1) // move to next tab -> b
	if m.active != "b" {
		t.Fatalf("active = %q, want b", m.active)
	}
	if m.tabs["b"].unread {
		t.Fatal("switching to b must clear its unread")
	}
}

func TestRenderEventShowsCostPerTab(t *testing.T) {
	m := newTestModel()
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "done", Done: true, Cost: 0.0042}})
	joined := strings.Join(m.tabs["a"].lines, "\n")
	if !strings.Contains(joined, "done") || !strings.Contains(joined, "$0.0042") {
		t.Fatalf("cost/reply dropped: %q", joined)
	}
}

func TestRenderEventMarksAbandonedPerTab(t *testing.T) {
	m := newTestModel()
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "abandoned"}})
	if !strings.Contains(strings.Join(m.tabs["a"].lines, "\n"), "abandoned") {
		t.Fatal("abandoned not surfaced")
	}
}

// fakeBackend satisfies the Backend interface for unit tests.
type fakeBackend struct {
	dispatched [][]string
	sessions   []contracts.SessionInfo
	fe         chan RoutedEvent
}

func (f *fakeBackend) Frontend() <-chan RoutedEvent        { return f.fe }
func (f *fakeBackend) Submit(string, string)               {}
func (f *fakeBackend) Sessions() []contracts.SessionInfo   { return f.sessions }
func (f *fakeBackend) Dispatch(args []string) (string, error) {
	f.dispatched = append(f.dispatched, args)
	return "ok", nil
}

func TestSlashLineDispatches(t *testing.T) {
	f := &fakeBackend{}
	m := newModel(f)
	m.ensureTab("a")
	m.input.SetValue("/session list")
	m.handleEnter()
	if len(f.dispatched) != 1 || f.dispatched[0][0] != "session" {
		t.Fatalf("slash line not dispatched: %+v", f.dispatched)
	}
}

func TestPlainLineSubmits(t *testing.T) {
	f := &fakeBackend{}
	m := newModel(f)
	m.ensureTab("a")
	m.input.SetValue("hello world")
	m.handleEnter()
	if len(f.dispatched) != 0 {
		t.Fatalf("plain line must not dispatch: %+v", f.dispatched)
	}
}

func TestClosedEventRemovesTab(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.ensureTab("a")
	m.ensureTab("b")
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "b"}, Event: contracts.Event{T: "closed"}})
	if _, ok := m.tabs["b"]; ok {
		t.Fatal("closed event must remove tab b")
	}
	for _, ch := range m.order {
		if ch == "b" {
			t.Fatal("closed tab still in order")
		}
	}
}

func TestSyncTabsFromSessions(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "alpha", ChannelID: "terminal/alpha-1"}}}
	m := newModel(f)
	m.syncTabs()
	tb, ok := m.tabs["terminal/alpha-1"]
	if !ok || tb.label != "alpha" {
		t.Fatalf("tab not synced/labelled from Sessions(): %+v", m.tabs)
	}
}
