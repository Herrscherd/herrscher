package host

import (
	"context"
	"fmt"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	orchestrator "github.com/Herrscherd/herrscher-orchestrator"
)

type restoreVerbMem struct {
	nodes      map[string]contracts.Node
	lastQuery  contracts.Query
	lastUnlink [2]string
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

func (m *restoreVerbMem) Search(_ context.Context, q contracts.Query) ([]contracts.Node, error) {
	m.lastQuery = q
	var hits []contracts.Node
	for _, n := range m.nodes {
		hits = append(hits, n)
	}
	return hits, nil
}
func (m *restoreVerbMem) Links(context.Context, string, string, string) error { return nil }
func (m *restoreVerbMem) Unlink(_ context.Context, from, to string) error {
	m.lastUnlink = [2]string{from, to}
	return nil
}
func (m *restoreVerbMem) Close() error { return nil }

func useRestoreMem(t *testing.T, m *restoreVerbMem) *restoreVerbMem {
	t.Helper()
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:     "fake-restore-verb",
			Category: contracts.CategoryMemory,
		},
		Memory: func(context.Context, contracts.PluginConfig) (contracts.Memory, error) {
			return m, nil
		},
	})
	return m
}

func TestMemoryRestoreVerbReactivatesArchivedNode(t *testing.T) {
	mem := useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{
		"facts/a": {Key: "facts/a", Meta: map[string]string{
			contracts.MetaState:    contracts.StateArchived,
			contracts.MetaLastSeen: "2020-01-01T00:00:00Z",
		}},
	}})
	reg, err := NewRegistry(context.Background(), nil, Options{StatePath: t.TempDir() + "/s.json"})
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
	got := mem.nodes["facts/a"]
	if got.Meta[contracts.MetaState] != contracts.StateActive {
		t.Errorf("state = %q, want active", got.Meta[contracts.MetaState])
	}
}

func TestMemoryRestoreVerbForceDetachesMergedOriginal(t *testing.T) {
	mem := useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{
		"facts/a": {Key: "facts/a", Meta: map[string]string{
			orchestrator.MetaMergedInto: "facts/u",
			contracts.MetaState:         contracts.StateArchived,
			contracts.MetaLastSeen:      "2020-01-01T00:00:00Z",
		}},
	}})
	reg, err := NewRegistry(context.Background(), nil, Options{StatePath: t.TempDir() + "/s.json"})
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
	got := mem.nodes["facts/a"]
	if got.Meta[contracts.MetaState] != contracts.StateActive || got.Meta[orchestrator.MetaMergedInto] != "" {
		t.Fatalf("force-restore did not detach: %+v", got.Meta)
	}
}

func TestMemoryRestoreVerbBareForceDetaches(t *testing.T) {
	mem := useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{
		"facts/a": {Key: "facts/a", Meta: map[string]string{
			orchestrator.MetaMergedInto: "facts/u",
			contracts.MetaState:         contracts.StateArchived,
			contracts.MetaLastSeen:      "2020-01-01T00:00:00Z",
		}},
	}})
	reg, err := NewRegistry(context.Background(), nil, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Dispatch(context.Background(), []string{"memory", "restore", "--key", "facts/a", "--force"})
	if err != nil {
		t.Fatalf("dispatch with bare --force: %v", err)
	}
	if out != "restored facts/a" {
		t.Errorf("output = %q, want %q", out, "restored facts/a")
	}
	got := mem.nodes["facts/a"]
	if got.Meta[contracts.MetaState] != contracts.StateActive || got.Meta[orchestrator.MetaMergedInto] != "" {
		t.Fatalf("bare-force restore did not detach: %+v", got.Meta)
	}
}

func TestMemoryUnlinkVerbDispatchesToMemory(t *testing.T) {
	mem := useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{}})
	reg, err := NewRegistry(context.Background(), nil, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Dispatch(context.Background(), []string{"memory", "unlink", "--from", "facts/a", "--to", "facts/b"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if out != "unlinked facts/a -> facts/b" {
		t.Errorf("output = %q, want %q", out, "unlinked facts/a -> facts/b")
	}
	if mem.lastUnlink != [2]string{"facts/a", "facts/b"} {
		t.Errorf("Unlink not dispatched, got %+v", mem.lastUnlink)
	}
}

func TestMemoryRestoreVerbUnknownKeyErrors(t *testing.T) {
	useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{}})
	reg, err := NewRegistry(context.Background(), nil, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Dispatch(context.Background(), []string{"memory", "restore", "--key", "nope"}); err == nil {
		t.Fatal("expected an error restoring an unknown key")
	}
}
