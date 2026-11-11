package host

import (
	"context"
	"io"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/historywire"
)

func TestAwaitTurnPersistsSettledProject(t *testing.T) {
	from := make(chan contracts.Event, 1)
	d := newSessionDriver("s", nil, make(chan contracts.Event, 1), from, io.Discard)
	var gotResume, gotProject string
	d.sink.Resume = func(tok string) { gotResume = tok }
	d.sink.Project = func(p string) { gotProject = p }

	from <- contracts.Event{T: "reply", Text: "ok", Done: true, Resume: "sid-1", Project: "neublox"}
	if !d.awaitTurn(context.Background(), tokenGuard{}) {
		t.Fatal("awaitTurn should return true on reply{done}")
	}
	if gotResume != "sid-1" {
		t.Fatalf("resume = %q, want sid-1", gotResume)
	}
	if gotProject != "neublox" {
		t.Fatalf("project = %q, want neublox", gotProject)
	}
}

func TestAwaitTurnSkipsAnEmptyProject(t *testing.T) {
	from := make(chan contracts.Event, 1)
	d := newSessionDriver("s", nil, make(chan contracts.Event, 1), from, io.Discard)
	called := false
	d.sink.Project = func(string) { called = true }

	from <- contracts.Event{T: "reply", Text: "ok", Done: true}
	_ = d.awaitTurn(context.Background(), tokenGuard{})
	if called {
		t.Fatal("a turn that settled nothing must not pin anything")
	}
}

func TestAwaitTurnKeepsALateHistoryAnswerFromTheGateways(t *testing.T) {
	from := make(chan contracts.Event, 2)
	d := newSessionDriver("s", nil, make(chan contracts.Event, 1), from, io.Discard)
	var tapped []contracts.Event
	d.emitTap = func(e contracts.Event) { tapped = append(tapped, e) }

	from <- contracts.Event{T: historywire.EventHistory, Who: "3", Text: `{"messages":[]}`, Done: true}
	from <- contracts.Event{T: "reply", Text: "ok", Done: true}
	if !d.awaitTurn(context.Background(), tokenGuard{}) {
		t.Fatal("awaitTurn should return true on reply{done}")
	}
	for _, event := range tapped {
		if historywire.IsFrame(event.T) {
			t.Fatalf("the gateways received the history frame %+v", event)
		}
	}
}
