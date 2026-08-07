package host

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// TestMemorySearchVerbReturnsFormattedHits drives `memory search --text …`
// through the real registry dispatch path, reusing the package's only fake
// memory plugin (restoreVerbMem / currentRestoreMem, registered once in
// memory_restore_verb_test.go) so BuildFirstMemory's "first registered
// memory plugin" contract stays deterministic.
func TestMemorySearchVerbReturnsFormattedHits(t *testing.T) {
	useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{
		"facts/a": {Key: "facts/a", Kind: contracts.KindDecision, Title: "Some Decision"},
	}})
	reg, err := NewRegistry(context.Background(), nil, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Dispatch(context.Background(), []string{"memory", "search", "--text", "decision"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !strings.Contains(out, "facts/a") || !strings.Contains(out, "decision") || !strings.Contains(out, "Some Decision") {
		t.Errorf("output = %q, want key/kind/title present", out)
	}
}

// TestMemorySearchVerbRawFlag asserts --raw is forwarded as
// contracts.Query.IncludeRaw = true, and that it defaults to false without
// the flag.
func TestMemorySearchVerbRawFlag(t *testing.T) {
	useRestoreMem(t, &restoreVerbMem{nodes: map[string]contracts.Node{}})
	reg, err := NewRegistry(context.Background(), nil, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := reg.Dispatch(context.Background(), []string{"memory", "search", "--text", "x", "--raw"}); err != nil {
		t.Fatalf("dispatch with --raw: %v", err)
	}
	if !currentRestoreMem.lastQuery.IncludeRaw {
		t.Errorf("IncludeRaw = false, want true when --raw is set")
	}

	if _, err := reg.Dispatch(context.Background(), []string{"memory", "search", "--text", "x"}); err != nil {
		t.Fatalf("dispatch without --raw: %v", err)
	}
	if currentRestoreMem.lastQuery.IncludeRaw {
		t.Errorf("IncludeRaw = true, want false when --raw is absent")
	}
}
