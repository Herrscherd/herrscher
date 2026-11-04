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
	if instruction := sessions.lookup("api").takeCompactInstruction(); instruction != "garde les décisions" {
		t.Fatalf("queued instruction = %q, want the typed one", instruction)
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

func TestEmptyCompactRequestKeepsTheQueuedInstruction(t *testing.T) {
	store, sessions, queue := compactCmdFixture(t, vendorClaude)
	ctx := context.Background()
	focus := contracts.Input{Args: map[string]string{paramName: "api", "instruction": "focus sur l'API"}}
	empty := contracts.Input{Args: map[string]string{paramName: "api"}}
	if _, err := compactSession(ctx, store, sessions, focus); err != nil {
		t.Fatalf("first request: %v", err)
	}
	out, err := compactSession(ctx, store, sessions, empty)
	if err != nil {
		t.Fatalf("empty request: %v", err)
	}
	if want := "compaction de \"api\" déjà en file : fusionnée avec la demande en attente, la consigne en file est conservée"; out != want {
		t.Fatalf("empty request reply = %q, want %q", out, want)
	}
	<-queue
	if got := sessions.lookup("api").takeCompactInstruction(); got != "focus sur l'API" {
		t.Fatalf("instruction after an empty request = %q, want the queued one kept", got)
	}
}

func TestCompactRequestsForOneSessionAreMerged(t *testing.T) {
	store, sessions, queue := compactCmdFixture(t, vendorClaude)
	ctx := context.Background()
	request := func(instruction string) contracts.Input {
		return contracts.Input{Args: map[string]string{paramName: "api", "instruction": instruction}}
	}
	if out, err := compactSession(ctx, store, sessions, request("first")); err != nil || out != "" {
		t.Fatalf("first request = %q, %v, want no output and no error", out, err)
	}
	out, err := compactSession(ctx, store, sessions, request("latest"))
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if out == "" {
		t.Fatal("second request was queued again without telling the operator it was merged")
	}
	if n := len(queue); n != 1 {
		t.Fatalf("queue holds %d compactions for one session, want 1", n)
	}
	<-queue
	driver := sessions.lookup("api")
	if got := driver.takeCompactInstruction(); got != "latest" {
		t.Fatalf("instruction taken = %q, want the most recent one %q", got, "latest")
	}
	if _, err := compactSession(ctx, store, sessions, request("after")); err != nil {
		t.Fatalf("request after the queued one ran: %v", err)
	}
	if n := len(queue); n != 1 {
		t.Fatalf("a request after the pending one was taken queued %d items, want 1", n)
	}
}
