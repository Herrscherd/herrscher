package host

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestAwaitTurnPersistsResumeToken(t *testing.T) {
	from := make(chan contracts.Event, 1)
	d := newSessionDriver("s", nil, make(chan contracts.Event, 1), from)
	var got string
	d.persistResume = func(tok string) { got = tok }

	from <- contracts.Event{T: "reply", Text: "ok", Done: true, Resume: "sid-1"}
	if !d.awaitTurn(context.Background()) {
		t.Fatal("awaitTurn should return true on reply{done}")
	}
	if got != "sid-1" {
		t.Fatalf("persistResume: want sid-1, got %q", got)
	}
}

func TestAwaitTurnSkipsEmptyResumeToken(t *testing.T) {
	from := make(chan contracts.Event, 1)
	d := newSessionDriver("s", nil, make(chan contracts.Event, 1), from)
	called := false
	d.persistResume = func(string) { called = true }

	from <- contracts.Event{T: "reply", Text: "ok", Done: true} // no Resume
	_ = d.awaitTurn(context.Background())
	if called {
		t.Fatal("persistResume must not fire for an empty token")
	}
}
