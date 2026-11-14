package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
	bridge "github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestACloseRefusedWhileTheBridgeIsStillStoppingKeepsTheWorktree(t *testing.T) {
	h, _, sup, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "busy", ChannelID: "42", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
	sup.stopErrs = []error{bridge.ErrStillStopping}

	_, err := h.sessionCloseRun(context.Background(), args("name", "busy"))
	if !errors.Is(err, bridge.ErrStillStopping) {
		t.Fatalf("close = %v, want ErrStillStopping", err)
	}
	if _, ok := st.FindSession("busy"); !ok {
		t.Fatal("a close refused by a stuck bridge must keep the session")
	}
	if len(wt.removed) != 0 || len(wt.removedScrap) != 0 {
		t.Fatalf("the worktree was removed under a bridge still running in it: removed=%v scratch=%v", wt.removed, wt.removedScrap)
	}
	if len(sup.started) != 0 {
		t.Fatalf("started=%v, want no restart after a refused stop", sup.started)
	}
}
