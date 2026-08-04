package main

import (
	"context"
	"flag"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	orchestrator "github.com/Herrscherd/herrscher-orchestrator"
	"github.com/Herrscherd/herrscher/core/host"
)

// fakeExtractor is a no-op Extractor used only to register a name so the
// orchestrator builds its learning variant; the learning *behaviour* is the
// orchestrator's concern (tested upstream), B2 only wires the config through.
type fakeExtractor struct{}

func (fakeExtractor) Extract(ctx context.Context, journal, transcript string) ([]orchestrator.Candidate, error) {
	return nil, nil
}

func TestBuildOrchestratorLearnerWhenExtractorConfigured(t *testing.T) {
	orchestrator.RegisterExtractor("b2-fake", fakeExtractor{})
	orch := buildOrchestrator(context.Background(), nil, "sess", "", "",
		learnConfig{extractor: "b2-fake", journal: ".neublox/calls.log", consolidateEvery: 3}, host.Logger(false))
	if _, ok := orch.(*orchestrator.Learner); !ok {
		t.Fatalf("want *orchestrator.Learner when an extractor is configured, got %T", orch)
	}
}

func TestBuildOrchestratorCuratorWhenNoExtractor(t *testing.T) {
	orch := buildOrchestrator(context.Background(), nil, "sess", "", "",
		learnConfig{}, host.Logger(false))
	if _, ok := orch.(*orchestrator.Curator); !ok {
		t.Fatalf("want plain *orchestrator.Curator with no extractor, got %T", orch)
	}
}

// TestBackendFlagsCarryModelID pins the consuming half of the model-routing
// path: the supervisor now spawns `herrscher bridge --model <catalog id>`, and
// that id must land in host.BackendRequest.ModelID — the only field that makes
// BuildBackendFor resolve the route and inject gateway credentials. With it
// dropped (or the flag renamed), a gateway session spawns bare, on the
// machine's own vendor login.
func TestBackendFlagsCarryModelID(t *testing.T) {
	fs := flag.NewFlagSet("bridge", flag.ContinueOnError)
	f := declareBackendFlags(fs)
	if err := fs.Parse([]string{"--cmd", "claude", "--vendor", "claude", "--model", "gw-claude-opus-5"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	req := f.request()
	if req.ModelID != "gw-claude-opus-5" {
		t.Fatalf("BackendRequest.ModelID = %q, want the --model value", req.ModelID)
	}
	if req.Cmd != "claude" || req.Vendor != "claude" {
		t.Fatalf("other backend flags regressed: %+v", req)
	}

	fs = flag.NewFlagSet("bridge", flag.ContinueOnError)
	f = declareBackendFlags(fs)
	if err := fs.Parse([]string{"--cmd", "claude"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := f.request().ModelID; got != "" {
		t.Fatalf("legacy call with no --model produced ModelID %q", got)
	}
}

// remoteStub is a resolver that always yields an out-of-process proxy, i.e. a
// daemon started with HERRSCHER_REMOTE=backend.
type remoteStub struct{}

func (remoteStub) Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, error) {
	return stubBackend{}, nil
}

type stubBackend struct{}

func (stubBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "", nil
}
func (stubBackend) Close() error { return nil }

// A remote proxy cannot be handed a spawn environment, so it cannot carry a
// gateway model's credentials. That combination must fail loudly rather than
// spawn on the machine's own login.
func TestNewBackendForRefusesModelWithRemoteBackend(t *testing.T) {
	_, err := newBackendFor(context.Background(), remoteStub{}, host.BackendRequest{
		Cmd: "claude", ModelID: "gw-claude-opus-5",
	})
	if err == nil {
		t.Fatal("remote backend + catalog model must be refused")
	}
	if !strings.Contains(err.Error(), "gw-claude-opus-5") {
		t.Fatalf("error does not name the model: %v", err)
	}
}

// Non-regression: no model named, the remote proxy is still used.
func TestNewBackendForKeepsRemoteBackendWithoutModel(t *testing.T) {
	be, err := newBackendFor(context.Background(), remoteStub{}, host.BackendRequest{Cmd: "claude"})
	if err != nil {
		t.Fatalf("newBackendFor: %v", err)
	}
	if be == nil {
		t.Fatal("remote backend not returned")
	}
}
