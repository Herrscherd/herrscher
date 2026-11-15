//go:build unix

package host

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func resumeHubWith(t *testing.T, sess state.Session) (*hub, *state.State) {
	t.Helper()
	layout := isolatedRuntimeDir(t)
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := ensureRuntimeDir(layout, SessionCommandSocketPathIn(layout, "", sess.Name)); err != nil {
		t.Fatal(err)
	}
	answeringListener(t, SessionCommandSocketPathIn(layout, "", sess.Name))
	h := newHub(st, nil, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable())
	h.runtime = layout
	return h, st
}

func TestResumeKeepsAnArchivedSessionArchivedWhenItCannotGoLive(t *testing.T) {
	name := "resume-" + newTurnID()
	h, st := resumeHubWith(t, state.Session{Name: name, Archived: true, Incarnation: "incarnation"})
	if err := h.Resume(name); err == nil {
		t.Fatal("Resume with a taken command socket succeeded")
	}
	if sess, _ := st.FindSession(name); !sess.Archived {
		t.Fatal("a session that never went live was persisted as unarchived")
	}
}

func TestRestartWithResumeKeepsTheOldTokenWhenItCannotGoLive(t *testing.T) {
	name := "restart-" + newTurnID()
	h, st := resumeHubWith(t, state.Session{Name: name, ResumeToken: "old", Incarnation: "incarnation"})
	if err := h.RestartWithResume(name, "new"); err == nil {
		t.Fatal("RestartWithResume with a taken command socket succeeded")
	}
	if sess, _ := st.FindSession(name); sess.ResumeToken != "old" {
		t.Fatalf("resume token = %q after a failed restart, want the old one", sess.ResumeToken)
	}
}
