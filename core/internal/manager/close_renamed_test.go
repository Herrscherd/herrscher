package manager

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

func TestARenamedSessionClosesTheWorktreeItWasCreatedWith(t *testing.T) {
	h, wt, repo := renamedSessionHandler(t)
	path := createSessionWorktree(t, h, wt, repo, "alpha")
	renameSession(t, h, "alpha", "beta")

	if _, err := h.sessionCloseRun(context.Background(), args("name", "beta")); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the worktree %s must be gone, stat err = %v", path, err)
	}
	if out := gitOutput(t, repo, "branch", "--list", "session/inst/alpha"); out == "" {
		t.Fatal("closing keeps the session branch session/inst/alpha")
	}
}

func TestARefusedCloseOfARenamedSessionNamesItsRealBranch(t *testing.T) {
	h, wt, repo := renamedSessionHandler(t)
	path := createSessionWorktree(t, h, wt, repo, "alpha")
	if err := os.WriteFile(filepath.Join(path, "wip.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	renameSession(t, h, "alpha", "beta")

	_, err := h.sessionCloseRun(context.Background(), args("name", "beta"))
	if err == nil {
		t.Fatal("a dirty worktree must refuse the close")
	}
	kept, ok := errors.AsType[*dirtyWorktreeError](err)
	if !ok || kept.KeptBranch() != "session/inst/alpha" {
		t.Fatalf("refusal %v, want it to keep session/inst/alpha", err)
	}
	if _, err := os.Stat(filepath.Join(path, "wip.txt")); err != nil {
		t.Fatalf("the refused close must leave the work in place: %v", err)
	}
}

func TestARenamedScratchSessionClosesItsScratchDirectory(t *testing.T) {
	h, wt, repo := renamedSessionHandler(t)
	path, err := wt.Scratch(repo, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.AddSession(state.Session{Name: "alpha", Worktree: path, Dir: path, Scratch: true}); err != nil {
		t.Fatal(err)
	}
	renameSession(t, h, "alpha", "beta")

	if _, err := h.sessionCloseRun(context.Background(), args("name", "beta")); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the scratch directory %s must be gone, stat err = %v", path, err)
	}
}

func renamedSessionHandler(t *testing.T) (*Handler, *worktree.Worktreer, string) {
	t.Helper()
	repo := t.TempDir()
	for _, gitArgs := range [][]string{
		{"init"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		gitOutput(t, repo, gitArgs...)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("/.herrscher-sessions/\n/.herrscher-scratch/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, repo, "add", ".gitignore")
	gitOutput(t, repo, "commit", "-m", "ignore")
	messages := clitext.NewMessages()
	wt := worktree.NewWorktreer(messages, testInstance)
	st := state.NewState(filepath.Join(t.TempDir(), "s.json"))
	st.InstanceID = testInstance
	if err := st.SetWorkspace(repo); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(messages, &fakeChannelAdmin{}, &fakeSup{}, wt, &fakeForge{}, &fakeUpdater{}, agent.NewStore(t.TempDir()), st, "claude", t.TempDir(), []string{"chat"}, nil, nil)
	return h, wt, repo
}

func createSessionWorktree(t *testing.T, h *Handler, wt *worktree.Worktreer, repo, name string) string {
	t.Helper()
	path, err := wt.Create(context.Background(), repo, name, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.AddSession(state.Session{Name: name, Worktree: path, Dir: path}); err != nil {
		t.Fatal(err)
	}
	return path
}

func renameSession(t *testing.T, h *Handler, from, to string) {
	t.Helper()
	if _, err := h.sessionRenameRun(context.Background(), args("name", from, "to", to)); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

func gitOutput(t *testing.T, dir string, gitArgs ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, gitArgs...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", gitArgs, err, out)
	}
	return string(out)
}
