package manager

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionJSONRowStatus(t *testing.T) {
	if r := sessionJSONRow(state.Session{Name: "a"}); r.Status != "running" {
		t.Fatalf("live row status = %q, want running", r.Status)
	}
	if r := sessionJSONRow(state.Session{Name: "a", Archived: true}); r.Status != "archived" {
		t.Fatalf("archived row status = %q, want archived", r.Status)
	}
	if got := sessionJSONRow(state.Session{Name: "a", ResumeToken: "tok"}); !got.Resumable {
		t.Fatalf("ResumeToken set → Resumable must be true")
	}
	if got := sessionJSONRow(state.Session{Name: "b"}); got.Resumable {
		t.Fatalf("no ResumeToken → Resumable must be false")
	}
}

// archive keeps the row + transcript + resume token and marks it archived;
// close (destructive) removes the transcript file.
func TestArchiveKeepsCloseRemovesTranscript(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "keep")); err != nil {
		t.Fatal(err)
	}
	tp := state.TranscriptPath(h.PartDir(), "keep")
	if err := state.AppendTranscript(tp, state.TranscriptEntry{Ts: "t", Role: "user", Text: "hi"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionArchiveRun(context.Background(), args("name", "keep")); err != nil {
		t.Fatalf("archive: %v", err)
	}
	sess, ok := st.FindSession("keep")
	if !ok {
		t.Fatalf("archive must keep the row")
	}
	if !sess.Archived {
		t.Fatalf("archive must set Archived")
	}
	if got := state.ReadTranscript(tp, 0); len(got) != 1 {
		t.Fatalf("archive must keep the transcript, got %d entries", len(got))
	}
	if len(sup.stopped) == 0 {
		t.Fatalf("archive must stop the supervised child")
	}

	if _, err := h.sessionCloseRun(context.Background(), args("name", "keep")); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, ok := st.FindSession("keep"); ok {
		t.Fatalf("close must remove the row")
	}
	if got := state.ReadTranscript(tp, 0); got != nil {
		t.Fatalf("close must purge the transcript, got %v", got)
	}
}
