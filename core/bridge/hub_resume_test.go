package bridge

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// resumableBackend is a fakeBackend that also reports a resume token.
type resumableBackend struct {
	fakeBackend
	token string
}

func (b resumableBackend) ResumeToken() string { return b.token }

func TestRunHubReplyCarriesResumeToken(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, resumableBackend{fakeBackend: fakeBackend{reply: "ok"}, token: "sid-1"}, nil)

	last := sink.events[len(sink.events)-1]
	if last.T != "reply" || !last.Done || last.Resume != "sid-1" {
		t.Fatalf("reply must carry resume sid-1; got %+v", last)
	}
}

func TestRunHubReplyResumeEmptyWhenNotResumeAware(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, fakeBackend{reply: "ok"}, nil)

	last := sink.events[len(sink.events)-1]
	if last.Resume != "" {
		t.Fatalf("non-ResumeAware backend must leave Resume empty; got %q", last.Resume)
	}
}
