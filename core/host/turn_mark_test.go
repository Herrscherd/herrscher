package host

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func newMarkHub(t *testing.T, partDir string, sess state.Session) (*hub, *state.State) {
	t.Helper()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	return testHub(&hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: io.Discard, st: st, partDir: partDir, live: make(map[string]liveSession)}), st
}

func TestAnnounceInterruptedTurnAppendsNoticeAndClearsMark(t *testing.T) {
	partDir := t.TempDir()
	h, st := newMarkHub(t, partDir, state.Session{Name: "main", InterruptedTurn: "turn-1"})
	h.announceInterruptedTurn(state.Session{Name: "main", InterruptedTurn: "turn-1"})

	entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10)
	if len(entries) != 1 || entries[0].Role != "notice" || entries[0].Kind != "turn_interrupted" || entries[0].Text == "" {
		t.Fatalf("transcript = %+v, want one turn_interrupted notice", entries)
	}
	if got, _ := st.FindSession("main"); got.InterruptedTurn != "" {
		t.Fatalf("mark = %q after announce, want cleared", got.InterruptedTurn)
	}
}

func TestAnnounceInterruptedTurnWithoutMarkWritesNothing(t *testing.T) {
	partDir := t.TempDir()
	h, _ := newMarkHub(t, partDir, state.Session{Name: "main"})
	h.announceInterruptedTurn(state.Session{Name: "main"})

	if entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10); len(entries) != 0 {
		t.Fatalf("transcript = %+v, want nothing without a mark", entries)
	}
}

func TestAnnounceInterruptedTurnKeepsMarkWhenTranscriptFails(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, st := newMarkHub(t, blocked, state.Session{Name: "main", InterruptedTurn: "turn-1"})
	h.announceInterruptedTurn(state.Session{Name: "main", InterruptedTurn: "turn-1"})

	if got, _ := st.FindSession("main"); got.InterruptedTurn != "turn-1" {
		t.Fatalf("mark = %q after a failed announce, want kept for the next start", got.InterruptedTurn)
	}
}

func TestRestartWithResumeDoesNotAnnounceTheMarkOfTheOldConversation(t *testing.T) {
	name := "restore-" + newTurnID()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: name, ResumeToken: "old", InterruptedTurn: "turn-1", Incarnation: "incarnation"}); err != nil {
		t.Fatal(err)
	}
	partDir := t.TempDir()
	h := newHub(st, nil, nil, partDir, nil, nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages()), withWarnings(io.Discard))
	h.runtime = isolatedRuntimeDir(t)
	h.startBridge = func(state.Session, time.Duration) error { return nil }

	if err := h.RestartWithResume(name, "new"); err != nil {
		t.Fatalf("RestartWithResume: %v", err)
	}
	if entries := state.ReadTranscript(state.TranscriptPath(partDir, name), 10); len(entries) != 0 {
		t.Fatalf("transcript = %+v after a restore, want no notice of the old conversation", entries)
	}
	sess, _ := st.FindSession(name)
	if sess.InterruptedTurn != "" {
		t.Fatalf("mark = %q after a restore, want cleared", sess.InterruptedTurn)
	}
	if sess.ResumeToken != "new" {
		t.Fatalf("resume token = %q, want the restored one", sess.ResumeToken)
	}
}

func stateFailingAfter(t *testing.T, sess state.Session) *state.State {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(path)
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return st
}

func hubWarningInto(t *testing.T, st *state.State, partDir string, warnings *lockedBuffer) *hub {
	t.Helper()
	h := newHub(st, nil, nil, partDir, nil, nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages()), withWarnings(warnings))
	h.runtime = isolatedRuntimeDir(t)
	h.startBridge = func(state.Session, time.Duration) error { return nil }
	return h
}

func TestKeepResumeTokenWarnsOnceWhenItCannotPersist(t *testing.T) {
	warnings := &lockedBuffer{}
	st := stateFailingAfter(t, state.Session{Name: "main"})
	h := hubWarningInto(t, st, t.TempDir(), warnings)
	h.keepResumeToken("main", "tok")
	awaitSinkQuiet(t, h.sink)
	if n := strings.Count(warnings.String(), "\n"); n != 1 || !strings.Contains(warnings.String(), `"main"`) {
		t.Fatalf("warnings = %q, want exactly one line naming the session", warnings.String())
	}
}

func TestOpenTurnMarkWarnsWhenItCannotWrite(t *testing.T) {
	warnings := &lockedBuffer{}
	st := stateFailingAfter(t, state.Session{Name: "main"})
	h := hubWarningInto(t, st, t.TempDir(), warnings)
	h.openTurnMark("main", "turn-1")
	awaitSinkQuiet(t, h.sink)
	if n := strings.Count(warnings.String(), "\n"); n != 1 || !strings.Contains(warnings.String(), `"main"`) {
		t.Fatalf("warnings = %q, want exactly one line naming the session", warnings.String())
	}
}

func TestAnnounceInterruptedTurnWarnsWhenTheMarkCannotBeCleared(t *testing.T) {
	warnings := &lockedBuffer{}
	st := stateFailingAfter(t, state.Session{Name: "main", InterruptedTurn: "turn-1"})
	partDir := t.TempDir()
	h := hubWarningInto(t, st, partDir, warnings)
	h.announceInterruptedTurn(state.Session{Name: "main", InterruptedTurn: "turn-1"})
	if entries := state.ReadTranscript(state.TranscriptPath(partDir, "main"), 10); len(entries) != 1 {
		t.Fatalf("transcript = %+v, want the notice written once", entries)
	}
	awaitSinkQuiet(t, h.sink)
	if n := strings.Count(warnings.String(), "\n"); n != 1 || !strings.Contains(warnings.String(), `"main"`) {
		t.Fatalf("warnings = %q, want exactly one line naming the session", warnings.String())
	}
}

func TestResumeOfAnArchivedSessionAnnouncesItsMark(t *testing.T) {
	name := "archived-" + newTurnID()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: name, Archived: true, InterruptedTurn: "turn-1", Incarnation: "incarnation"}); err != nil {
		t.Fatal(err)
	}
	partDir := t.TempDir()
	h := hubWarningInto(t, st, partDir, &lockedBuffer{})
	if err := h.Resume(name); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	entries := state.ReadTranscript(state.TranscriptPath(partDir, name), 10)
	if len(entries) != 1 || entries[0].Kind != "turn_interrupted" {
		t.Fatalf("transcript = %+v, want the interruption notice at revive", entries)
	}
}
