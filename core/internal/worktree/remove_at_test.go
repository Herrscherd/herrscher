package worktree

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestRemoveAtTellsToForceWhenTheRecordedPathOutsideTheRootIsGone(t *testing.T) {
	repo := initRepo(t)
	gone := filepath.Join(repo, sessionsDir, "alpha")
	w := NewWorktreer(clitext.NewMessages(), "inst")

	err := w.RemoveAt(t.Context(), repo, gone, false)
	if !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("err = %v, want %v", err, ErrOutsideRoot)
	}
	failure, ok := errors.AsType[FailureError](err)
	if !ok || failure.Key != clitext.WorktreeOutsideRootGone {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.WorktreeOutsideRootGone)
	}
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		if text := failure.Text(lang); !strings.Contains(text, "force:true") {
			t.Errorf("refusal in %v is %q, want it to say to close with force:true", lang, text)
		}
	}
	if err := w.RemoveAt(t.Context(), repo, gone, true); err != nil {
		t.Fatalf("forced close of a vanished path outside the root: %v", err)
	}
}
