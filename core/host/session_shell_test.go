package host

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func shellFixture(t *testing.T, mode string) (*state.State, *approvalTable, *sessionTable, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if err := st.AddSession(state.Session{Name: "s1", ChannelID: "c1", Dir: dir}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if mode != "" {
		if err := st.SetSessionApprovals("s1", mode); err != nil {
			t.Fatalf("SetSessionApprovals: %v", err)
		}
	}
	return st, newApprovalTable(io.Discard), newSessionTable(), dir
}

func TestSessionShellRunsInTheSessionDirAndReturnsOutput(t *testing.T) {
	st, approvals, sessions, dir := shellFixture(t, "bypass")
	out, err := runSessionShell(context.Background(), testLanguage(), io.Discard, st, nil, approvals, sessions, "s1", "pwd; echo bonjour")
	if err != nil {
		t.Fatalf("runSessionShell: %v", err)
	}
	if !strings.Contains(out, dir) || !strings.Contains(out, "bonjour") {
		t.Fatalf("output %q must show the session dir and the echo", out)
	}
}

func TestSessionShellReportsExitCode(t *testing.T) {
	st, approvals, sessions, _ := shellFixture(t, "bypass")
	out, err := runSessionShell(context.Background(), testLanguage(), io.Discard, st, nil, approvals, sessions, "s1", "echo oops; exit 3")
	if err != nil {
		t.Fatalf("runSessionShell: %v", err)
	}
	if !strings.Contains(out, "oops") || !strings.Contains(out, "[sortie 3]") {
		t.Fatalf("output %q must keep the text and the exit code", out)
	}
}

func TestSessionShellGoesThroughTheApprovalPolicy(t *testing.T) {
	st, approvals, sessions, dir := shellFixture(t, "strict")
	unanswered, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := runSessionShell(unanswered, testLanguage(), io.Discard, st, nil, approvals, sessions, "s1", "touch "+filepath.Join(dir, "x"))
	if !errors.Is(err, errShellRefused) {
		t.Fatalf("a strict session must refuse an unapproved command, got %v", err)
	}
}

func TestSessionShellRefusesEmptyAndUnknown(t *testing.T) {
	st, approvals, sessions, _ := shellFixture(t, "bypass")
	if _, err := runSessionShell(context.Background(), testLanguage(), io.Discard, st, nil, approvals, sessions, "s1", "   "); err == nil {
		t.Fatal("an empty command must be refused")
	}
	if _, err := runSessionShell(context.Background(), testLanguage(), io.Discard, st, nil, approvals, sessions, "nope", "ls"); err == nil {
		t.Fatal("an unknown session must be refused")
	}
}

func TestSessionShellRefusesARemoteSessionBeforeAnyApproval(t *testing.T) {
	st, approvals, sessions, dir := shellFixture(t, "bypass")
	if err := st.AddSession(state.Session{Name: "distante", ChannelID: "c2", Dir: dir, Host: "box-distante"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	marker := filepath.Join(dir, "touched")
	_, err := runSessionShell(t.Context(), testLanguage(), io.Discard, st, nil, approvals, sessions, "distante", "touch "+marker)
	if !errors.Is(err, errRemoteShellSession) {
		t.Fatalf("a remote session must be refused, got %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("a refused remote command must not run")
	}
}

func TestSessionShellReturnsWhenABackgroundChildHoldsOutput(t *testing.T) {
	st, approvals, sessions, _ := shellFixture(t, "bypass")
	started := time.Now()
	out, err := runSessionShell(t.Context(), testLanguage(), io.Discard, st, nil, approvals, sessions, "s1", "sleep 6 & echo lance")
	if err != nil {
		t.Fatalf("runSessionShell: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the call waited %v for a background child that kept the output pipe", elapsed)
	}
	if !strings.Contains(out, "lance") {
		t.Fatalf("output %q must keep the text of the command", out)
	}
}
