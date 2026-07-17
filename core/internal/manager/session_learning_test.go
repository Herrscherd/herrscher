package manager

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// TestSessionCreatePersistsLearningConfig is the host-side end of the P1 write
// path: `session create` with extractor/journal/consolidate_every must land on
// the persisted Session, so the supervisor can thread them to the bridge and the
// orchestrator builds a Learner instead of the plain Curator.
func TestSessionCreatePersistsLearningConfig(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo",
		"extractor", "roblox",
		"journal", ".neublox/calls.log",
		"consolidate_every", "5",
	)); err != nil {
		t.Fatal(err)
	}

	sess, ok := st.FindSession("demo")
	if !ok {
		t.Fatalf("session not persisted; sessions=%+v", st.SnapshotSessions())
	}
	if sess.Extractor != "roblox" || sess.Journal != ".neublox/calls.log" || sess.ConsolidateEvery != 5 {
		t.Fatalf("learning config not persisted: %+v", sess)
	}
}

func TestSessionCreatePersistsVendor(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "vendor", "cursor")); err != nil {
		t.Fatal(err)
	}
	sess, ok := st.FindSession("demo")
	if !ok || sess.Vendor != "cursor" {
		t.Fatalf("session Vendor = %q, ok=%v; want cursor", sess.Vendor, ok)
	}
}

// TestSessionCreateRejectsBadConsolidateEvery guards the only parsed integer.
func TestSessionCreateRejectsBadConsolidateEvery(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "consolidate_every", "-1",
	)); err == nil {
		t.Fatal("expected error on negative consolidate_every")
	}
	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo2", "consolidate_every", "abc",
	)); err == nil {
		t.Fatal("expected error on non-integer consolidate_every")
	}
}
