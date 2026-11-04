package host

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func compactFixture(t *testing.T, vendor string) (*state.State, *sessionTable, chan queued) {
	t.Helper()
	store := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := store.AddSession(state.Session{Name: "api", Vendor: vendor}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	queue := make(chan queued, 1)
	sessions := newSessionTable()
	sessions.register("api", &sessionDriver{name: "api", queue: queue})
	return store, sessions, queue
}

func TestCompactSendsSlashCommandToClaudeSession(t *testing.T) {
	store, sessions, queue := compactFixture(t, vendorClaude)
	input := contracts.Input{Args: map[string]string{paramName: "api", "origin": "terminal"}}

	if _, err := compactSession(context.Background(), store, sessions, input); err != nil {
		t.Fatalf("compactSession: %v", err)
	}
	got := <-queue
	if got.ev.Text != compactCommand {
		t.Fatalf("queued text = %q, want %q", got.ev.Text, compactCommand)
	}
	if got.origin.Gateway != "terminal" {
		t.Fatalf("origin gateway = %q, want terminal", got.origin.Gateway)
	}
}

func TestCompactRefusesOtherVendors(t *testing.T) {
	store, sessions, queue := compactFixture(t, backendCodex)
	input := contracts.Input{Args: map[string]string{paramName: "api"}}

	_, err := compactSession(context.Background(), store, sessions, input)
	if err == nil || !strings.Contains(err.Error(), "only available on claude") {
		t.Fatalf("compactSession error = %v, want a claude-only refusal", err)
	}
	if len(queue) != 0 {
		t.Fatalf("a refused compaction must not reach the turn queue")
	}
}

func TestCompactUnknownSession(t *testing.T) {
	store, sessions, _ := compactFixture(t, vendorClaude)
	input := contracts.Input{Args: map[string]string{paramName: "ghost"}}

	if _, err := compactSession(context.Background(), store, sessions, input); err == nil {
		t.Fatal("compactSession on an unknown session must fail")
	}
}
