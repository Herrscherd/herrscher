package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// Closing a session stops its bridge first — it has to, the worktree cannot be
// removed from under a running process. But when the removal then fails the
// session survives, and nothing else in the daemon ever restarts a bridge: it
// would stay listed, keep its control socket, and swallow every message sent to
// it until the daemon was restarted. A refused close must leave the session
// exactly as it found it.
func TestARefusedCloseLeavesTheSessionAnswering(t *testing.T) {
	h, _, sup, wt, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	st.AddSession(state.Session{Name: "dirty", ChannelID: "42", Type: "text", Worktree: "/wt/x"})
	wt.removeErr = errors.New("uncommitted changes")

	if _, err := h.sessionCloseRun(context.Background(), args("name", "dirty")); err == nil {
		t.Fatal("a dirty worktree must refuse the close")
	}
	if _, ok := st.FindSession("dirty"); !ok {
		t.Fatal("the refused close must keep the session")
	}
	if len(sup.started) != 1 || sup.started[0] != "dirty" {
		t.Fatalf("started=%v, want the bridge put back after the refusal", sup.started)
	}
}

// Forcing through the same dirty worktree closes for real, and does not leave a
// bridge behind for a session that is gone.
func TestAForcedCloseStillRemovesEverything(t *testing.T) {
	h, _, sup, wt, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	st.AddSession(state.Session{Name: "dirty", ChannelID: "42", Type: "text", Worktree: "/wt/x"})
	wt.removeErr = errors.New("uncommitted changes")

	if _, err := h.sessionCloseRun(context.Background(), args("name", "dirty", "force", "true")); err != nil {
		t.Fatalf("force close: %v", err)
	}
	if _, ok := st.FindSession("dirty"); ok {
		t.Fatal("the session should be gone")
	}
	if len(sup.started) != 0 {
		t.Fatalf("started=%v, want no bridge restarted for a closed session", sup.started)
	}
}
