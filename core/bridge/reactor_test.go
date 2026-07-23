package bridge

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// reactingOrch is a minimal Orchestrator that also implements TurnReactor,
// stripping a sentinel marker from the reply so the hub test can assert React ran.
type reactingOrch struct{ reacted bool }

func (*reactingOrch) Context(context.Context) string { return "" }
func (*reactingOrch) Observe(context.Context, contracts.Prompt, string) error {
	return nil
}
func (*reactingOrch) Consolidate(context.Context) error { return nil }
func (*reactingOrch) Close() error                      { return nil }
func (o *reactingOrch) React(_ context.Context, reply string) string {
	o.reacted = true
	return strings.ReplaceAll(reply, "<mark>", "")
}

func TestHubCallsReactWhenOrchIsReactor(t *testing.T) {
	orch := &reactingOrch{}
	resp := &captureBackend{reply: "hello <mark>world"}
	sink := &recordSink{}
	runOneTurn(context.Background(), sink, resp, orch, contracts.Event{T: "input", Text: "hi"}, nil, nil)
	if !orch.reacted {
		t.Fatalf("React was not invoked")
	}
	last := sink.events[len(sink.events)-1]
	if last.T != "reply" || last.Text != "hello world" {
		t.Fatalf("reply not passed through React, got %+v", last)
	}
}

// plainOrch implements only Orchestrator, not TurnReactor.
type plainOrch struct{}

func (plainOrch) Context(context.Context) string                          { return "" }
func (plainOrch) Observe(context.Context, contracts.Prompt, string) error { return nil }
func (plainOrch) Consolidate(context.Context) error                       { return nil }
func (plainOrch) Close() error                                            { return nil }

func TestHubUnaffectedByPlainOrchestrator(t *testing.T) {
	resp := &captureBackend{reply: "just text"}
	sink := &recordSink{}
	runOneTurn(context.Background(), sink, resp, plainOrch{}, contracts.Event{T: "input", Text: "hi"}, nil, nil)
	last := sink.events[len(sink.events)-1]
	if last.Text != "just text" {
		t.Fatalf("plain orchestrator reply altered: %+v", last)
	}
}
