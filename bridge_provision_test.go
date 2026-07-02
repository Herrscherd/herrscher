package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

// recordingMem is a fake Memory that also implements contracts.Provisioner,
// capturing the ensured roots.
type recordingMem struct {
	projects [][2]string // {key, title}
	agents   [][2]string
}

func (m *recordingMem) Recall(context.Context, string, int) (contracts.Subgraph, error) {
	return contracts.Subgraph{}, nil
}
func (m *recordingMem) Record(context.Context, contracts.Node) error { return nil }
func (m *recordingMem) Search(context.Context, contracts.Query) ([]contracts.Node, error) {
	return nil, nil
}
func (m *recordingMem) Links(context.Context, string, string, string) error { return nil }
func (m *recordingMem) Close() error                                        { return nil }
func (m *recordingMem) EnsureProject(_ context.Context, key, title string) error {
	m.projects = append(m.projects, [2]string{key, title})
	return nil
}
func (m *recordingMem) EnsureAgent(_ context.Context, key, title string) error {
	m.agents = append(m.agents, [2]string{key, title})
	return nil
}

func TestProvisionScopeEnsuresBothRoots(t *testing.T) {
	m := &recordingMem{}
	provisionScope(context.Background(), m, "game", "scripter", slog.Default())

	if len(m.projects) != 1 || m.projects[0] != [2]string{"projects/game", "game"} {
		t.Fatalf("project root not ensured: %+v", m.projects)
	}
	if len(m.agents) != 1 || m.agents[0] != [2]string{"agents/scripter", "scripter"} {
		t.Fatalf("agent root not ensured: %+v", m.agents)
	}
}

func TestProvisionScopeSkipsEmptyNames(t *testing.T) {
	m := &recordingMem{}
	provisionScope(context.Background(), m, "", "", slog.Default())
	if len(m.projects) != 0 || len(m.agents) != 0 {
		t.Fatalf("empty names must ensure nothing: %+v %+v", m.projects, m.agents)
	}
}

func TestProvisionScopeIgnoresNonProvisioner(t *testing.T) {
	// A Memory that is not a Provisioner must be handled without panicking.
	var mem contracts.Memory = &struct{ contracts.Memory }{}
	provisionScope(context.Background(), mem, "game", "scripter", slog.Default())
}
