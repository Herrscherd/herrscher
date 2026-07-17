package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/host"
)

func mkReg(t *testing.T) *cli.Registry {
	t.Helper()
	reg, err := host.NewRegistry(context.Background(), host.Deps{}, host.Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestMemoryLocateVerb(t *testing.T) {
	t.Setenv("OBSIDIAN_VAULT", t.TempDir())
	reg := mkReg(t)
	ctx := context.Background()
	if _, err := reg.Dispatch(ctx, []string{"memory", "record", "--key", "a/b", "--kind", "decision", "--title", "T"}); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Dispatch(ctx, []string{"memory", "locate", "--key", "a/b", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	var loc contracts.Location
	if err := json.Unmarshal([]byte(out), &loc); err != nil {
		t.Fatalf("bad json %q: %v", out, err)
	}
	if !strings.HasSuffix(loc.File, "/a/b.md") {
		t.Fatalf("File = %q", loc.File)
	}
	if !strings.Contains(loc.Obsidian, "obsidian://open?vault=") {
		t.Fatalf("Obsidian = %q", loc.Obsidian)
	}
}

func TestMemoryForgetVerb(t *testing.T) {
	t.Setenv("OBSIDIAN_VAULT", t.TempDir())
	reg := mkReg(t)
	ctx := context.Background()
	if _, err := reg.Dispatch(ctx, []string{"memory", "record", "--key", "x", "--kind", "decision"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Dispatch(ctx, []string{"memory", "forget", "--key", "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Dispatch(ctx, []string{"memory", "locate", "--key", "x"}); err == nil {
		t.Fatal("expected locate to fail after forget")
	}
}
