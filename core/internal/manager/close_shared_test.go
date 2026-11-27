package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestClosingASessionWhoseFolderAnotherSessionRecordsIsRefusedEvenForced(t *testing.T) {
	h, wt, repo := homedRenamedHandler(t)
	if _, err := h.sessionCreateRun(context.Background(), args("name", "alpha")); err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	folder := wt.Path(repo, "alpha")
	work := filepath.Join(folder, "work.txt")
	if err := os.WriteFile(work, []byte("beta works here"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.st.AddSession(state.Session{Name: "beta", ChannelID: "c-beta", Worktree: folder, Dir: folder}); err != nil {
		t.Fatal(err)
	}

	_, err := h.sessionCloseRun(context.Background(), args("name", "alpha", "force", "true"))
	if !errors.Is(err, ErrWorkspaceShared) {
		t.Fatalf("forced close of alpha = %v, want %v", err, ErrWorkspaceShared)
	}
	if _, statErr := os.Stat(work); statErr != nil {
		t.Fatalf("the refused close removed the folder beta works in: %v", statErr)
	}
	if _, found := h.st.FindSession("alpha"); !found {
		t.Fatal("the refused close dropped alpha from the state")
	}
}
