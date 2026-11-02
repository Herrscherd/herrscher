package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestARefusedCloseLeavesTheSessionAnswering(t *testing.T) {
	h, _, sup, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "dirty", ChannelID: "42", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
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

func TestAForcedCloseStillRemovesEverything(t *testing.T) {
	h, _, sup, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "dirty", ChannelID: "42", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
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
