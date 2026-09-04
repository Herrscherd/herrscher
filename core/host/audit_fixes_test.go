package host

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

func TestAnswerApprovalPickIsSessionScoped(t *testing.T) {
	cases := []struct {
		name     string
		owner    string
		clicker  string
		swallowd bool
		answered bool
	}{
		{name: "own session answers", owner: "alpha", clicker: "alpha", swallowd: true, answered: true},
		{name: "other session cannot answer", owner: "alpha", clicker: "beta", swallowd: true, answered: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &pendingApproval{
				id:      newApprovalID(),
				session: tc.owner,
				req:     approval.Request{Tool: "Bash"},
				asked:   time.Now(),
				answer:  make(chan approvalVerdict, 1),
			}
			release, refusal := registerApproval(p)
			if refusal != "" {
				t.Fatalf("registerApproval refused: %s", refusal)
			}
			defer release()
			if got := answerApprovalPick(tc.clicker, approvalPickPrefix+p.id+":allow"); got != tc.swallowd {
				t.Fatalf("answerApprovalPick = %v, want %v", got, tc.swallowd)
			}
			select {
			case v := <-p.answer:
				if !tc.answered {
					t.Fatalf("request settled by a foreign session: %v", v.decision)
				}
			default:
				if tc.answered {
					t.Fatal("request not settled by its own session")
				}
			}
		})
	}
}

func TestDriverEnqueueIsBounded(t *testing.T) {
	prev := enqueueTimeout
	enqueueTimeout = 50 * time.Millisecond
	defer func() { enqueueTimeout = prev }()

	cases := []struct {
		name string
		call func(*sessionDriver) bool
	}{
		{name: "seed", call: func(d *sessionDriver) bool { return d.Seed("task") }},
		{name: "pick", call: func(d *sessionDriver) bool { return d.Pick("value") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newSessionDriver("full", nil, make(chan contracts.Event, 1), make(chan contracts.Event))
			for len(d.queue) < cap(d.queue) {
				d.queue <- queued{ev: contracts.Event{T: "input"}}
			}
			done := make(chan bool, 1)
			go func() { done <- tc.call(d) }()
			select {
			case ok := <-done:
				if ok {
					t.Fatal("enqueue reported success on a full queue")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("enqueue blocked forever on a full queue")
			}
		})
	}
}

func TestGoDeadDoesNotHoldTheHubLockWhileWaiting(t *testing.T) {
	prev := sessionTeardownGrace
	sessionTeardownGrace = 100 * time.Millisecond
	defer func() { sessionTeardownGrace = prev }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stuck := make(chan struct{})
	h := &hub{ctx: ctx, live: map[string]liveSession{}}
	h.live["wedged"] = liveSession{cancel: func() {}, done: stuck}

	probed := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Millisecond)
		h.mu.Lock()
		h.mu.Unlock()
		close(probed)
	}()

	returned := make(chan struct{})
	go func() { h.goDead("wedged"); close(returned) }()

	select {
	case <-probed:
	case <-time.After(time.Second):
		t.Fatal("h.mu held while waiting for a wedged teardown")
	}
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("goDead never returned on a wedged teardown")
	}
	h.mu.Lock()
	_, still := h.live["wedged"]
	h.mu.Unlock()
	if still {
		t.Fatal("wedged session name never released")
	}
}

func TestApprovalIDsAreRandomAndWellFormed(t *testing.T) {
	seen := make(map[string]bool, 4096)
	for range 4096 {
		id := newApprovalID()
		if len(id) != 8 {
			t.Fatalf("newApprovalID() = %q, want 8 hex characters", id)
		}
		if _, err := hex.DecodeString(id); err != nil {
			t.Fatalf("newApprovalID() = %q, not hex: %v", id, err)
		}
		if seen[id] {
			t.Fatalf("newApprovalID() repeated %q within 4096 draws", id)
		}
		seen[id] = true
	}
}
