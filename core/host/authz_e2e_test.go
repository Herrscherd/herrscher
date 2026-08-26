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

// TestTheSameArgvIsRefusedOnOneSocketAndRunOnTheOther is the whole claim, with
// nothing faked: two real listeners over one real registry, the same bytes sent
// to each. Every other test checks a piece of this in isolation; this one
// proves the pieces still line up once they are wired together, and that
// identity really does come from the listener rather than from anything the
// message could have said.
func TestTheSameArgvIsRefusedOnOneSocketAndRunOnTheOther(t *testing.T) {
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if err := st.AddSession(state.Session{Name: "s1"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	reg := &cli.Registry{}
	if err := addApproveCommands(reg, st, nil); err != nil {
		t.Fatalf("addApproveCommands: %v", err)
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

	// And the rule the operator added is the only one there: the refusal did
	// not half-run.
	if got := len(st.ApprovalRules()); got != 1 {
		t.Fatalf("%d rules stored, want 1", got)
	}
}
