//go:build !windows

package host

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type dispatcherFunc func(context.Context, []string) (string, error)

func (f dispatcherFunc) Dispatch(ctx context.Context, argv []string) (string, error) {
	return f(ctx, argv)
}

func TestHookThroughTheCommandSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	approvalTab := newApprovalTable()
	reg := newApprovalRegistry(t, approvalTab)
	sock := startApprovalSocket(ctx, t, reg)

	t.Setenv(SessionVar, "s1")
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"git push origin master"}}`)
	var out, errOut bytes.Buffer
	done := runHookAsync(ctx, in, &out, &errOut, sock)

	id := awaitPendingApproval(t, approvalTab, "s1")
	if _, err := reg.Dispatch(ctx, []string{"approve", "deny", id, "--reason", "not today"}); err != nil {
		t.Fatalf("deny: %v", err)
	}
	<-done
	assertHookDeniedWithReason(t, out.Bytes(), "not today")
}

func newApprovalRegistry(t *testing.T, approvalTab *approvalTable) *cli.Registry {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{"sessions":[],"approvalTimeout":"3s"}`), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	st, err := state.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if err := st.AddSession(state.Session{Name: "s1", ChannelID: "c1"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := st.AddApprovalRule("ask Bash(git push*)"); err != nil {
		t.Fatalf("AddApprovalRule: %v", err)
	}
	reg := &cli.Registry{}
	if err := addApproveCommands(reg, st, nil, approvalTab, newSessionTable()); err != nil {
		t.Fatalf("addApproveCommands: %v", err)
	}
	return reg
}

func startApprovalSocket(ctx context.Context, t *testing.T, reg *cli.Registry) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "cmd.sock")
	serveCommandSocket(ctx, sock, dispatcherFunc(reg.Dispatch))
	return sock
}

func runHookAsync(ctx context.Context, in io.Reader, out, errOut io.Writer, sock string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunPermissionHook(ctx, in, out, errOut, func() string { return sock }, func(c context.Context, argv []string) (string, bool, error) {
			return dispatchLiveCommand(c, sock, argv)
		})
	}()
	return done
}

func awaitPendingApproval(t *testing.T, approvalTab *approvalTable, session string) string {
	t.Helper()
	var id string
	waitFor(t, &approvalTab.changed, func() bool {
		for _, p := range pendingApprovals(approvalTab) {
			if p.Session == session {
				id = p.ID
			}
		}
		return id != ""
	}, "the hook reached the daemon")
	return id
}

func assertHookDeniedWithReason(t *testing.T, raw []byte, reason string) {
	t.Helper()
	var got struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("hook output %q: %v", raw, err)
	}
	if got.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("got %q, want deny", got.HookSpecificOutput.PermissionDecision)
	}
	if !strings.Contains(got.HookSpecificOutput.PermissionDecisionReason, reason) {
		t.Fatalf("reason %q must carry what the operator typed", got.HookSpecificOutput.PermissionDecisionReason)
	}
}
