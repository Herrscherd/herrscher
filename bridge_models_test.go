package main

import (
	"flag"
	"path/filepath"
	"testing"
)

func TestBridgeReadsTheCacheBesideItsStateFile(t *testing.T) {
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "state.json")
	flags := declareBackendFlags(flag.NewFlagSet("bridge", flag.ContinueOnError))
	got := flags.request(statePath).Models.CachePath()
	want := filepath.Join(stateDir, "models-discovered.json")
	if got != want {
		t.Fatalf("bridge model cache = %q, want %q (beside --state)", got, want)
	}
}
