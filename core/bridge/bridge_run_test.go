package bridge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type startOrch struct{ started atomic.Bool }

func (o *startOrch) Context(context.Context) string                          { return "" }
func (o *startOrch) Observe(context.Context, contracts.Prompt, string) error { return nil }
func (o *startOrch) Consolidate(context.Context) error                       { return nil }
func (o *startOrch) Close() error                                            { return nil }
func (o *startOrch) Start(context.Context)                                   { o.started.Store(true) }

type noopOrchestrator struct{}

func (noopOrchestrator) Context(context.Context) string                          { return "" }
func (noopOrchestrator) Observe(context.Context, contracts.Prompt, string) error { return nil }
func (noopOrchestrator) Consolidate(context.Context) error                       { return nil }
func (noopOrchestrator) Close() error                                            { return nil }

func failBackend(string) (contracts.Backend, error) {
	return nil, errors.New("no backend in test")
}

func TestRunStartsOptionalCapability(t *testing.T) {
	orch := &startOrch{}
	opts := Options{Channel: "c", HubSocket: "/nonexistent-hub.sock", ApprovalWait: time.Minute}
	if err := Run(context.Background(), failBackend, orch, opts); err != nil {
		t.Logf("run: %v", err)
	}
	if !orch.started.Load() {
		t.Fatal("Run did not invoke the orchestrator's optional Start capability")
	}
}

func TestRunRefusesToStartWithoutAnApprovalWait(t *testing.T) {
	opts := Options{Channel: "c", HubSocket: "/nonexistent-hub.sock"}
	if err := Run(context.Background(), failBackend, noopOrchestrator{}, opts); !errors.Is(err, errNoApprovalWait) {
		t.Fatalf("got %v, want errNoApprovalWait: a bridge with no bound must not wait forever", err)
	}
}

func TestRunWithoutStartCapabilityStillRuns(t *testing.T) {
	opts := Options{Channel: "c", HubSocket: "/nonexistent-hub.sock", ApprovalWait: time.Minute}
	if err := Run(context.Background(), failBackend, noopOrchestrator{}, opts); err == nil {
		t.Fatal("expected Run to surface the backend error")
	}
}
