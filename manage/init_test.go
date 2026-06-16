package manage

import (
	"reflect"
	"testing"
)

func TestSetPlugins(t *testing.T) {
	mods := []string{"example.com/a", "example.com/b"}
	out, err := setPlugins(sample, mods)
	if err != nil {
		t.Fatal(err)
	}
	got, err := listPlugins(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, mods) {
		t.Fatalf("got %v, want %v", got, mods)
	}
}

func TestResolveStackDefault(t *testing.T) {
	mods, err := resolveStack(defaultStack, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		catalog["gateway"]["discord"],
		catalog["backend"]["claude"],
		catalog["memory"]["obsidian"],
		catalog["orchestrator"]["basic"],
	}
	if !reflect.DeepEqual(mods, want) {
		t.Fatalf("got %v, want %v", mods, want)
	}
}

func TestResolveStackNoneDropsCategory(t *testing.T) {
	choices := map[string]string{"gateway": "discord", "backend": "none", "memory": "none", "orchestrator": "none"}
	mods, err := resolveStack(choices, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 1 || mods[0] != catalog["gateway"]["discord"] {
		t.Fatalf("unexpected: %v", mods)
	}
}

func TestResolveStackExtrasDeduped(t *testing.T) {
	choices := map[string]string{"gateway": "discord"}
	mods, err := resolveStack(choices, []string{"example.com/x", catalog["gateway"]["discord"]})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{catalog["gateway"]["discord"], "example.com/x"}
	if !reflect.DeepEqual(mods, want) {
		t.Fatalf("got %v, want %v", mods, want)
	}
}

func TestResolveStackUnknownKind(t *testing.T) {
	if _, err := resolveStack(map[string]string{"gateway": "bogus"}, nil); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestResolveStackEmpty(t *testing.T) {
	choices := map[string]string{"gateway": "none", "backend": "none", "memory": "none", "orchestrator": "none"}
	if _, err := resolveStack(choices, nil); err == nil {
		t.Fatal("expected error for empty stack")
	}
}
