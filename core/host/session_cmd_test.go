package host

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func compactCmdFixture(t *testing.T, vendor string) (*state.State, *sessionTable, chan queued) {
	t.Helper()
	store := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := store.AddSession(state.Session{Name: "api", Vendor: vendor}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	queue := make(chan queued, 1)
	sessions := newSessionTable()
	sessions.register("api", &sessionDriver{
		name:           "api",
		vendor:         vendor,
		queue:          queue,
		enqueueTimeout: time.Second,
		compaction:     compactPolicy(defaultCompactPercent),
	})
	return store, sessions, queue
}

func TestCompactQueuesAManualCompactionWithItsInstruction(t *testing.T) {
	store, sessions, queue := compactCmdFixture(t, vendorClaude)
	input := contracts.Input{Args: map[string]string{paramName: "api", "instruction": "garde les décisions", "origin": "terminal"}}

	if _, err := compactSession(context.Background(), store, sessions, input); err != nil {
		t.Fatalf("compactSession: %v", err)
	}
	got := <-queue
	if got.ev.T != eventCompactNow {
		t.Fatalf("queued event = %q, want %q", got.ev.T, eventCompactNow)
	}
	if got.ev.Text != "garde les décisions" {
		t.Fatalf("queued instruction = %q, want the typed one", got.ev.Text)
	}
	if got.origin.Gateway != "terminal" {
		t.Fatalf("origin gateway = %q, want terminal", got.origin.Gateway)
	}
}

func TestCompactRefusesBackendWithoutCompactor(t *testing.T) {
	store, sessions, queue := compactCmdFixture(t, backendCodex)
	input := contracts.Input{Args: map[string]string{paramName: "api"}}

	_, err := compactSession(context.Background(), store, sessions, input)
	if !errors.Is(err, errCompactUnsupported) {
		t.Fatalf("compactSession error = %v, want errCompactUnsupported", err)
	}
	if len(queue) != 0 {
		t.Fatalf("a refused compaction must not reach the turn queue")
	}
}

func TestCompactUnknownSession(t *testing.T) {
	store, sessions, _ := compactCmdFixture(t, vendorClaude)
	input := contracts.Input{Args: map[string]string{paramName: "ghost"}}

	if _, err := compactSession(context.Background(), store, sessions, input); err == nil {
		t.Fatal("compactSession on an unknown session must fail")
	}
}
