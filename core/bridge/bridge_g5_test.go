package bridge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type startOrch struct{ started atomic.Bool }

func (o *startOrch) Context(context.Context) string                          { return "" }
func (o *startOrch) Observe(context.Context, contracts.Prompt, string) error { return nil }
func (o *startOrch) Consolidate(context.Context) error                       { return nil }
func (o *startOrch) Close() error                                            { return nil }
func (o *startOrch) Start(context.Context)                                   { o.started.Store(true) }

type plainOrchG5 struct{}

func (plainOrchG5) Context(context.Context) string                          { return "" }
func (plainOrchG5) Observe(context.Context, contracts.Prompt, string) error { return nil }
func (plainOrchG5) Consolidate(context.Context) error                       { return nil }
func (plainOrchG5) Close() error                                            { return nil }

func failBackend(string) (contracts.Backend, error) {
	return nil, errors.New("no backend in test")
}

func TestRunStartsOptionalCapability(t *testing.T) {
	orch := &startOrch{}
	opts := Options{Channel: "c", HubSocket: "/nonexistent-hub.sock"}
	if err := Run(context.Background(), failBackend, orch, opts); err != nil {
		t.Logf("run: %v", err)
	}
	if !orch.started.Load() {
		t.Fatal("Run did not invoke the orchestrator's optional Start capability")
	}
}

func TestRunWithoutStartCapabilityStillRuns(t *testing.T) {
	opts := Options{Channel: "c", HubSocket: "/nonexistent-hub.sock"}
	if err := Run(context.Background(), failBackend, plainOrchG5{}, opts); err == nil {
		t.Fatal("expected Run to surface the backend error")
	}
}
