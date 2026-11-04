package host

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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
	return st, newApprovalTable(), newSessionTable(), dir
}

func TestSessionShellRunsInTheSessionDirAndReturnsOutput(t *testing.T) {
	st, approvals, sessions, dir := shellFixture(t, "bypass")
	out, err := runSessionShell(context.Background(), st, nil, approvals, sessions, "s1", "pwd; echo bonjour")
	if err != nil {
		t.Fatalf("runSessionShell: %v", err)
	}
	if !strings.Contains(out, dir) || !strings.Contains(out, "bonjour") {
		t.Fatalf("output %q must show the session dir and the echo", out)
	}
}

func TestSessionShellReportsExitCode(t *testing.T) {
	st, approvals, sessions, _ := shellFixture(t, "bypass")
	out, err := runSessionShell(context.Background(), st, nil, approvals, sessions, "s1", "echo oops; exit 3")
	if err != nil {
		t.Fatalf("runSessionShell: %v", err)
	}
	if !strings.Contains(out, "oops") || !strings.Contains(out, "[sortie 3]") {
		t.Fatalf("output %q must keep the text and the exit code", out)
	}
}

func TestSessionShellGoesThroughTheApprovalPolicy(t *testing.T) {
	st, approvals, sessions, dir := shellFixture(t, "strict")
	_, err := runSessionShell(context.Background(), st, nil, approvals, sessions, "s1", "touch "+filepath.Join(dir, "x"))
	if !errors.Is(err, errShellRefused) {
		t.Fatalf("a strict session must refuse an unapproved command, got %v", err)
	}
}

func TestSessionShellRefusesEmptyAndUnknown(t *testing.T) {
	st, approvals, sessions, _ := shellFixture(t, "bypass")
	if _, err := runSessionShell(context.Background(), st, nil, approvals, sessions, "s1", "   "); err == nil {
		t.Fatal("an empty command must be refused")
	}
	if _, err := runSessionShell(context.Background(), st, nil, approvals, sessions, "nope", "ls"); err == nil {
		t.Fatal("an unknown session must be refused")
	}
}
