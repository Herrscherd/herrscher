//go:build !windows

package host

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type escalationRig struct {
	store     *state.State
	approvals *approvalTable
	socket    string
}

func newEscalationRig(t *testing.T) escalationRig {
	t.Helper()
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if addErr := st.AddSession(state.Session{Name: "s1"}); addErr != nil {
		t.Fatalf("AddSession: %v", addErr)
	}
	reg := cli.NewRegistry(clitext.NewMessages())
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages())
	if approveErr := addApproveCommands(reg, newHostMessages(), clitext.NewMessages(), fixedLanguage(i18n.English), io.Discard, st, nil, approvalTab, sessionTab); approveErr != nil {
		t.Fatalf("addApproveCommands: %v", approveErr)
	}
	desk := newApprovalDesk(newHostMessages(), fixedLanguage(i18n.English), io.Discard, st, nil, approvalTab, sessionTab)
	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: io.Discard, st: st, reg: reg, desk: desk, live: make(map[string]liveSession)})
	socket := filepath.Join(t.TempDir(), "s1.sock")
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	serveTestCommandSocket(ctx, t, isolatedRuntimeDir(t), socket, asSession{disp: h, session: "s1", lang: h.sessionLanguageSource("s1"), messages: clitext.NewMessages()}, defaultCommandReadTimeout)
	return escalationRig{store: st, approvals: approvalTab, socket: socket}
}

func (r escalationRig) dispatchAsync(t *testing.T, argv []string) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, _, err := dispatchLiveCommand(t.Context(), newHostMessages(), isolatedRuntimeDir(t), r.socket, argv)
		done <- err
	}()
	return done
}

func TestAVerbTheAgentMayNotRunIsPutToTheOperatorAndRunsOnAllow(t *testing.T) {
	rig := newEscalationRig(t)
	done := rig.dispatchAsync(t, []string{"approve", "rule", "allow Read(*)"})

	pending := awaitPendingLive(t, rig.approvals, 1)[0]
	if pending.Session != "s1" || pending.Tool != approval.VerbTool || pending.Subject != "approve rule allow Read(*)" {
		t.Fatalf("pending = %+v, want the verb asked as Herrscher(approve rule allow Read(*)) for s1", pending)
	}
	if !answerApproval(rig.approvals, pending.ID, approval.Allow) {
		t.Fatal("the question vanished before the operator answered")
	}
	if err := <-done; err != nil {
		t.Fatalf("the allowed verb failed: %v", err)
	}
	if got := rig.store.ApprovalRules(); len(got) != 1 {
		t.Fatalf("rules = %v, want the one the operator allowed", got)
	}
}

func TestAVerbTheOperatorRefusesNeverRuns(t *testing.T) {
	rig := newEscalationRig(t)
	done := rig.dispatchAsync(t, []string{"approve", "rule", "allow Bash(*)"})

	pending := awaitPendingLive(t, rig.approvals, 1)[0]
	if !answerApproval(rig.approvals, pending.ID, approval.Deny) {
		t.Fatal("the question vanished before the operator answered")
	}
	if err := <-done; err == nil {
		t.Fatal("a refused verb must come back as an error")
	}
	if got := rig.store.ApprovalRules(); len(got) != 0 {
		t.Fatalf("rules = %v, want none after a refusal", got)
	}
}

func TestAnsweringAnApprovalIsRefusedWithoutAQuestion(t *testing.T) {
	rig := newEscalationRig(t)
	err := <-rig.dispatchAsync(t, []string{"approve", "allow", "abc"})
	if err == nil {
		t.Fatal("a session must not answer an approval")
	}
	if pending := pendingApprovals(rig.approvals); len(pending) != 0 {
		t.Fatalf("pending = %+v, want no question: a session cannot ask to answer its own", pending)
	}
}
