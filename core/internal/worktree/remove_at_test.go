package worktree

import (
	"errors"
	"os"
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

func TestRemoveAtSaysHowToCloseASessionWhoseFolderOutsideTheRootStillExists(t *testing.T) {
	repo := initRepo(t)
	kept := filepath.Join(repo, sessionsDir, "alpha")
	if err := os.MkdirAll(kept, 0o750); err != nil {
		t.Fatal(err)
	}
	w := NewWorktreer(clitext.NewMessages(), "inst")

	for _, force := range []bool{false, true} {
		err := w.RemoveAt(t.Context(), repo, kept, force)
		failure, ok := errors.AsType[FailureError](err)
		if !ok || failure.Key != clitext.WorktreeOutsideRoot || !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("RemoveAt(force=%v) = %v, want a %s refusal", force, err, clitext.WorktreeOutsideRoot)
		}
		for _, lang := range []i18n.Language{i18n.English, i18n.French} {
			if text := failure.Text(lang); !strings.Contains(text, kept) || !strings.Contains(text, "force:true") {
				t.Errorf("refusal in %v is %q, want it to name the folder and say to close with force:true once it is moved", lang, text)
			}
		}
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("the refused folder is gone: %v", err)
	}
}
