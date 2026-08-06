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
	// lastQuery captures the most recent contracts.Query passed to Search, so
	// tests can assert on how the `memory search` verb built it (e.g.
	// IncludeRaw for --raw).
	lastQuery contracts.Query
	// lastUnlink captures the most recent (from, to) passed to Unlink, so the
	// `memory unlink` verb test can assert dispatch.
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

// currentRestoreMem is swapped by each test before building a registry.
// Registered exactly once below (init) as the package's only fake memory
// plugin, so BuildFirstMemory's "first registered memory plugin" contract
// deterministically resolves to whatever the current test set.
var currentRestoreMem *restoreVerbMem

// useRestoreMem installs a fixture for the duration of one test and takes it
// back down afterwards. Leaving it standing would hand the next test that builds
// a registry — most of this package — another test's memory contents, which is
// how a passing suite can hide a test that cannot pass alone.
func useRestoreMem(t *testing.T, m *restoreVerbMem) {
	t.Helper()
	currentRestoreMem = m
	t.Cleanup(func() { currentRestoreMem = nil })
}

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "fake-restore-verb", Category: contracts.CategoryMemory},
		Memory: func(context.Context, contracts.PluginConfig) (contracts.Memory, error) {
			// Never hand back a nil *restoreVerbMem: it would arrive as a non-nil
			// contracts.Memory holding a nil pointer, and panic on first Recall. Most
			// tests in this package build a registry without caring about memory at
			// all, and they must get an empty one that works rather than a mine.
			if currentRestoreMem == nil {
				return &restoreVerbMem{nodes: map[string]contracts.Node{}}, nil
			}
			return currentRestoreMem, nil
		},
	})
}

func TestMemoryRestoreVerbReactivatesArchivedNode(t *testing.T) {
	useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{
		"facts/a": {Key: "facts/a", Meta: map[string]string{
			contracts.MetaState:    contracts.StateArchived,
			contracts.MetaLastSeen: "2020-01-01T00:00:00Z",
		}},
	}})
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
	useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{
		"facts/a": {Key: "facts/a", Meta: map[string]string{
			orchestrator.MetaMergedInto: "facts/u",
			contracts.MetaState:         contracts.StateArchived,
			contracts.MetaLastSeen:      "2020-01-01T00:00:00Z",
		}},
	}})
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

// TestMemoryRestoreVerbBareForceDetaches covers the documented bare-flag idiom
// (`--force` with no following value), which the README advertises. The verb
// declares force as a valueless Param, so a bare --force must toggle it on.
func TestMemoryRestoreVerbBareForceDetaches(t *testing.T) {
	useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{
		"facts/a": {Key: "facts/a", Meta: map[string]string{
			orchestrator.MetaMergedInto: "facts/u",
			contracts.MetaState:         contracts.StateArchived,
			contracts.MetaLastSeen:      "2020-01-01T00:00:00Z",
		}},
	}})
	reg, err := NewRegistry(context.Background(), Deps{}, Options{StatePath: t.TempDir() + "/s.json"})
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
	got := currentRestoreMem.nodes["facts/a"]
	if got.Meta[contracts.MetaState] != contracts.StateActive || got.Meta[orchestrator.MetaMergedInto] != "" {
		t.Fatalf("bare-force restore did not detach: %+v", got.Meta)
	}
}

func TestMemoryUnlinkVerbDispatchesToMemory(t *testing.T) {
	useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{}})
	reg, err := NewRegistry(context.Background(), Deps{}, Options{StatePath: t.TempDir() + "/s.json"})
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
	if currentRestoreMem.lastUnlink != [2]string{"facts/a", "facts/b"} {
		t.Errorf("Unlink not dispatched, got %+v", currentRestoreMem.lastUnlink)
	}
}

func TestMemoryRestoreVerbUnknownKeyErrors(t *testing.T) {
	useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{}})
	reg, err := NewRegistry(context.Background(), Deps{}, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Dispatch(context.Background(), []string{"memory", "restore", "--key", "nope"}); err == nil {
		t.Fatal("expected an error restoring an unknown key")
	}
}
