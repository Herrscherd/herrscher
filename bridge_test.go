package main

import (
	"context"
	"testing"

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
