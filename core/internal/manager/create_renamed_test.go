package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

func homedRenamedHandler(t *testing.T) (*Handler, *worktree.Worktreer, string) {
	t.Helper()
	h, wt, repo := renamedSessionHandler(t)
	if err := h.st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	return h, wt, repo
}

func TestANewSessionDoesNotTakeTheWorktreeARenamedSessionKept(t *testing.T) {
	h, wt, repo := homedRenamedHandler(t)
	if _, err := h.sessionCreateRun(context.Background(), args("name", "alpha")); err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	kept := wt.Path(repo, "alpha")
	wip := filepath.Join(kept, "wip.txt")
	if err := os.WriteFile(wip, []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	renameSession(t, h, "alpha", "beta")

	_, err := h.sessionCreateRun(context.Background(), args("name", "alpha"))
	if !errors.Is(err, ErrWorkspaceTaken) {
		t.Fatalf("err = %v, want %v", err, ErrWorkspaceTaken)
	}
	if _, found := h.st.FindSession("alpha"); found {
		t.Fatal("the refused create recorded a session alpha")
	}
	if _, statErr := os.Stat(wip); statErr != nil {
		t.Fatalf("the refused create touched the worktree of beta: %v", statErr)
	}
	beta, _ := h.st.FindSession("beta")
	if beta.Worktree != kept {
		t.Fatalf("beta records %q, want %q", beta.Worktree, kept)
	}
}

func TestANewSessionDoesNotTakeTheScratchARenamedSessionKept(t *testing.T) {
	h, _, _ := homedRenamedHandler(t)
	plain := t.TempDir()
	if err := h.st.SetWorkspace(plain); err != nil {
		t.Fatal(err)
	}
	if _, err := h.agents.Create(agent.CreateSpec{Name: "coder", Backend: "claude"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "notes", "agent", "coder")); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	notes, _ := h.st.FindSession("notes")
	if !notes.Scratch {
		t.Fatalf("notes = %+v, want a scratch session", notes)
	}
	renameSession(t, h, "notes", "journal")

	_, err := h.sessionCreateRun(context.Background(), args("name", "notes", "agent", "coder"))
	if !errors.Is(err, ErrWorkspaceTaken) {
		t.Fatalf("err = %v, want %v", err, ErrWorkspaceTaken)
	}
	if _, statErr := os.Stat(notes.Worktree); statErr != nil {
		t.Fatalf("the refused create touched the scratch of journal: %v", statErr)
	}
}

func TestClosingASessionRecordedOutsideTheRootKeepsItsOwnRefusal(t *testing.T) {
	h, _, repo := renamedSessionHandler(t)
	outside := filepath.Join(repo, "elsewhere")
	if err := os.MkdirAll(outside, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := h.st.AddSession(state.Session{Name: "alpha", Worktree: outside, Dir: outside}); err != nil {
		t.Fatal(err)
	}
	_, err := h.sessionCloseRun(context.Background(), args("name", "alpha"))
	if !errors.Is(err, worktree.ErrOutsideRoot) {
		t.Fatalf("err = %v, want %v", err, worktree.ErrOutsideRoot)
	}
	if _, dirty := errors.AsType[*dirtyWorktreeError](err); dirty {
		t.Fatalf("err = %v, want the outside-root refusal, not the dirty worktree one", err)
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("the refused close removed %s: %v", outside, statErr)
	}
}
