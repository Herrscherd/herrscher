package manager

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// resume flips Archived back to false and restarts the supervised child.
func TestSessionResumeRunUnarchivesAndStarts(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "keep")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionArchiveRun(context.Background(), args("name", "keep")); err != nil {
		t.Fatalf("archive: %v", err)
	}
	// archive stopped the child; clear the record so we can prove resume starts it.
	sup.started = nil

	if _, err := h.sessionResumeRun(context.Background(), args("name", "keep")); err != nil {
		t.Fatalf("resume: %v", err)
	}
	sess, ok := st.FindSession("keep")
	if !ok {
		t.Fatalf("resume must keep the row")
	}
	if sess.Archived {
		t.Fatalf("resume must clear Archived")
	}
	if len(sup.started) == 0 {
		t.Fatalf("resume must start the supervised child")
	}
}

// resuming an unknown session is a loud error (mirrors archive).
func TestSessionResumeRunUnknownSessionErrors(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "category")
	if _, err := h.sessionResumeRun(context.Background(), args("name", "ghost")); err == nil {
		t.Fatalf("expected error resuming an unknown session")
	}
}
