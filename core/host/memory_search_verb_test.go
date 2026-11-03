package host

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

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

func TestMemorySearchVerbRawFlag(t *testing.T) {
	mem := useRestoreMem(t, &restoreVerbMem{nodes: make(map[string]contracts.Node)})
	reg, err := NewRegistry(context.Background(), nil, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := reg.Dispatch(context.Background(), []string{"memory", "search", "--text", "x", "--raw"}); err != nil {
		t.Fatalf("dispatch with --raw: %v", err)
	}
	if !mem.lastQuery.IncludeRaw {
		t.Errorf("IncludeRaw = false, want true when --raw is set")
	}

	if _, err := reg.Dispatch(context.Background(), []string{"memory", "search", "--text", "x"}); err != nil {
		t.Fatalf("dispatch without --raw: %v", err)
	}
	if mem.lastQuery.IncludeRaw {
		t.Errorf("IncludeRaw = true, want false when --raw is absent")
	}
}
