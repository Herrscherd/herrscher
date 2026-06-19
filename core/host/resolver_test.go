package host

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type stubMem struct{ contracts.Memory }

func TestResolverLocalReturnsFactoryObject(t *testing.T) {
	want := &stubMem{}
	r := NewResolver(nil, "") // nil remote set => everything local
	got, err := r.Memory(context.Background(),
		[]contracts.Plugin{{
			Manifest: contracts.Manifest{Kind: "obsidian", Category: contracts.CategoryMemory},
			Memory:   func(context.Context, contracts.PluginConfig) (contracts.Memory, error) { return want, nil },
		}},
		func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != want {
		t.Fatalf("local resolve must return the factory object, got %v", got)
	}
}
