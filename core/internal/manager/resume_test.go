package manager

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionResumeRunUnarchivesAndStarts(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "keep")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionArchiveRun(context.Background(), args("name", "keep")); err != nil {
		t.Fatalf("archive: %v", err)
	}
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

func TestSessionResumeRunUnknownSessionErrors(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "category")
	if _, err := h.sessionResumeRun(context.Background(), args("name", "ghost")); err == nil {
		t.Fatalf("expected error resuming an unknown session")
	}
}
