package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	claude "github.com/Herrscherd/herrscher-claude-backend"
	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/bridge"
)

func TestAFrenchTurnReachesTheClaudeBackendInFrench(t *testing.T) {
	dir := t.TempDir()
	recorded := filepath.Join(dir, "stdin")
	fake := filepath.Join(dir, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ncat > "+recorded+"\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	backend, err := claude.NewBackend(context.Background(), claude.Config{Kind: "oneshot", Cmd: fake})
	if err != nil {
		t.Fatal(err)
	}
	input := make(chan contracts.Event, 2)
	input <- contracts.Event{T: "language", Value: "fr"}
	input <- contracts.Event{T: "input", Text: "bonjour"}
	out := make(chan contracts.Event, 8)
	newBackend := func(string) (contracts.Backend, error) { return backend, nil }
	if err = bridge.RunOneShot(context.Background(), newBackend, nil, "channel", input, out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(recorded)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Reply in French.\n\nbonjour"; string(got) != want {
		t.Fatalf("claude received %q, want %q", got, want)
	}
}
