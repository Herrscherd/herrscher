package host

import (
	"context"
	"fmt"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	orchestrator "github.com/Herrscherd/herrscher-orchestrator"
)

// restoreVerbMem is a minimal fake contracts.Memory for exercising `memory
// restore` end to end through the real registry dispatch path (no obsidian
// vault involved). Recall errors on an absent key, matching the real contract
// orchestrator.Restore relies on.
type restoreVerbMem struct {
	nodes map[string]contracts.Node
}

func (m *restoreVerbMem) Recall(_ context.Context, key string, _ int) (contracts.Subgraph, error) {
	n, ok := m.nodes[key]
	if !ok {
		return contracts.Subgraph{}, fmt.Errorf("restoreVerbMem: no node at key %q", key)
	}
	return contracts.Subgraph{Root: n}, nil
}
func (m *restoreVerbMem) Record(_ context.Context, n contracts.Node) error {
	m.nodes[n.Key] = n
	return nil
}
func (m *restoreVerbMem) Search(context.Context, contracts.Query) ([]contracts.Node, error) {
	return nil, nil
}
func (m *restoreVerbMem) Links(context.Context, string, string, string) error { return nil }
func (m *restoreVerbMem) Close() error                                        { return nil }

// currentRestoreMem is swapped by each test before building a registry.
// Registered exactly once below (init) as the package's only fake memory
// plugin, so BuildFirstMemory's "first registered memory plugin" contract
// deterministically resolves to whatever the current test set.
var currentRestoreMem *restoreVerbMem

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "fake-restore-verb", Category: contracts.CategoryMemory},
		Memory: func(context.Context, contracts.PluginConfig) (contracts.Memory, error) {
			return currentRestoreMem, nil
		},
	})
}

func TestMemoryRestoreVerbReactivatesArchivedNode(t *testing.T) {
	currentRestoreMem = &restoreVerbMem{nodes: map[string]contracts.Node{
		"facts/a": {Key: "facts/a", Meta: map[string]string{
			contracts.MetaState:    contracts.StateArchived,
			contracts.MetaLastSeen: "2020-01-01T00:00:00Z",
		}},
	}}
	reg, err := NewRegistry(context.Background(), Deps{}, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Dispatch(context.Background(), []string{"memory", "restore", "--key", "facts/a"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if out != "restored facts/a" {
		t.Errorf("output = %q, want %q", out, "restored facts/a")
	}
	got := currentRestoreMem.nodes["facts/a"]
	if got.Meta[contracts.MetaState] != contracts.StateActive {
		t.Errorf("state = %q, want active", got.Meta[contracts.MetaState])
	}
}

func TestMemoryRestoreVerbForceDetachesMergedOriginal(t *testing.T) {
	currentRestoreMem = &restoreVerbMem{nodes: map[string]contracts.Node{
		"facts/a": {Key: "facts/a", Meta: map[string]string{
			orchestrator.MetaMergedInto: "facts/u",
			contracts.MetaState:         contracts.StateArchived,
			contracts.MetaLastSeen:      "2020-01-01T00:00:00Z",
		}},
	}}
	reg, err := NewRegistry(context.Background(), Deps{}, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Dispatch(context.Background(), []string{"memory", "restore", "--key", "facts/a"})
	if err == nil || !strings.Contains(err.Error(), "restore with Force") {
		t.Fatalf("without --force expected ErrMergedOriginal, got %v", err)
	}
	out, err := reg.Dispatch(context.Background(), []string{"memory", "restore", "--key", "facts/a", "--force", "true"})
	if err != nil {
		t.Fatalf("dispatch with force: %v", err)
	}
	if out != "restored facts/a" {
		t.Errorf("output = %q, want %q", out, "restored facts/a")
	}
	got := currentRestoreMem.nodes["facts/a"]
	if got.Meta[contracts.MetaState] != contracts.StateActive || got.Meta[orchestrator.MetaMergedInto] != "" {
		t.Fatalf("force-restore did not detach: %+v", got.Meta)
	}
}

func TestMemoryRestoreVerbUnknownKeyErrors(t *testing.T) {
	currentRestoreMem = &restoreVerbMem{nodes: map[string]contracts.Node{}}
	reg, err := NewRegistry(context.Background(), Deps{}, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Dispatch(context.Background(), []string{"memory", "restore", "--key", "nope"}); err == nil {
		t.Fatal("expected an error restoring an unknown key")
	}
}
