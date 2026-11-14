package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
	bridge "github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestAnArchiveRefusedWhileTheBridgeIsStillStoppingKeepsTheSession(t *testing.T) {
	h, d, sup, wt, _, st := newTestHandler(t, "")
	if err := st.AddSession(state.Session{Name: "busy", ChannelID: "42", Owned: true, Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
	sup.stopErrs = []error{bridge.ErrStillStopping}

	_, err := h.sessionArchiveRun(context.Background(), args("name", "busy"))
	if !errors.Is(err, bridge.ErrStillStopping) {
		t.Fatalf("archive = %v, want ErrStillStopping", err)
	}
	sess, ok := st.FindSession("busy")
	if !ok || sess.Archived {
		t.Fatalf("an archive refused by a stuck bridge must leave the session unarchived: found=%v archived=%v", ok, sess.Archived)
	}
	if len(d.archived) != 0 {
		t.Fatalf("the channel was tidied away under a bridge still running: %v", d.archived)
	}
	if len(wt.removed) != 0 || len(wt.removedScrap) != 0 {
		t.Fatalf("the worktree was removed by a refused archive: removed=%v scratch=%v", wt.removed, wt.removedScrap)
	}
}

func TestARenameRefusedWhileTheBridgeIsStillStoppingKeepsTheName(t *testing.T) {
	h, _, sup, wt, _, st := newTestHandler(t, "")
	if err := st.AddSession(state.Session{Name: "busy", ChannelID: "42", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
	sup.stopErrs = []error{bridge.ErrStillStopping}

	_, err := h.sessionRenameRun(context.Background(), args("name", "busy", "to", "quiet"))
	if !errors.Is(err, bridge.ErrStillStopping) {
		t.Fatalf("rename = %v, want ErrStillStopping", err)
	}
	if _, ok := st.FindSession("busy"); !ok {
		t.Fatal("a rename refused by a stuck bridge must keep the old name")
	}
	if _, ok := st.FindSession("quiet"); ok {
		t.Fatal("a rename refused by a stuck bridge still created the new name")
	}
	if len(sup.started) != 0 {
		t.Fatalf("started=%v, want no restart after a refused stop", sup.started)
	}
	if len(wt.removed) != 0 || len(wt.removedScrap) != 0 {
		t.Fatalf("the worktree was touched by a refused rename: removed=%v scratch=%v", wt.removed, wt.removedScrap)
	}
}
