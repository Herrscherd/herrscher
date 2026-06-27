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
