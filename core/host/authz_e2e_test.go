//go:build !windows

package host

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestTheSameArgvIsRefusedOnOneSocketAndRunOnTheOther(t *testing.T) {
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if addErr := st.AddSession(state.Session{Name: "s1"}); addErr != nil {
		t.Fatalf("AddSession: %v", addErr)
	}
	reg := &cli.Registry{}
	approvalTab := newApprovalTable()
	sessionTab := newSessionTable()
	if approveErr := addApproveCommands(reg, st, nil, approvalTab, sessionTab); approveErr != nil {
		t.Fatalf("addApproveCommands: %v", approveErr)
	}
	h := &hub{st: st, reg: reg, live: map[string]liveSession{}}

	dir := t.TempDir()
	operatorSock := filepath.Join(dir, "op.sock")
	sessionSock := filepath.Join(dir, "s1.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveCommandSocket(ctx, operatorSock, asOperator{h})
	go serveCommandSocket(ctx, sessionSock, asSession{disp: h, session: "s1"})
	waitForSocket(t, operatorSock)
	waitForSocket(t, sessionSock)

	argv := []string{"approve", "rule", "allow Bash(*)"}

	out, _, err := dispatchLiveCommand(ctx, operatorSock, argv)
	if err != nil {
		t.Fatalf("the operator socket was refused: %v", err)
	}
	if out == "" {
		t.Fatal("the operator's call produced nothing")
	}

	_, _, err = dispatchLiveCommand(ctx, sessionSock, argv)
	if err == nil {
		t.Fatal("the same argv on the session socket must be refused")
	}
	if !strings.Contains(err.Error(), "approve rule") {
		t.Fatalf("refusal %q must name the verb", err)
	}

	if got := len(st.ApprovalRules()); got != 1 {
		t.Fatalf("%d rules stored, want 1", got)
	}
}
