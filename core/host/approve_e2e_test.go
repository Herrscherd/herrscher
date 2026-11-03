//go:build !windows

package host

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type dispatcherFunc func(context.Context, []string) (string, error)

func (f dispatcherFunc) Dispatch(ctx context.Context, argv []string) (string, error) {
	return f(ctx, argv)
}

func TestHookThroughTheCommandSocket(t *testing.T) {
	sessionTab := newSessionTable()
	approvalTab := newApprovalTable()
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
	if err := addApproveCommands(reg, st, nil, approvalTab, sessionTab); err != nil {
		t.Fatalf("addApproveCommands: %v", err)
	}

	sock := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveCommandSocket(ctx, sock, dispatcherFunc(reg.Dispatch))
	waitForSocket(t, sock)

	t.Setenv(SessionVar, "s1")
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"git push origin master"}}`)
	var out, errOut bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunPermissionHook(ctx, in, &out, &errOut, func() string { return sock }, func(c context.Context, argv []string) (string, bool, error) {
			return dispatchLiveCommand(c, sock, argv)
		})
	}()

	var id string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range pendingApprovals(approvalTab) {
			if p.Session == "s1" {
				id = p.ID
			}
		}
		if id != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("the hook never reached the daemon")
	}
	if _, err := reg.Dispatch(ctx, []string{"approve", "deny", id, "--reason", "not today"}); err != nil {
		t.Fatalf("deny: %v", err)
	}
	<-done

	var got struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("hook output %q: %v", out.String(), err)
	}
	if got.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("got %q, want deny", got.HookSpecificOutput.PermissionDecision)
	}
	if !strings.Contains(got.HookSpecificOutput.PermissionDecisionReason, "not today") {
		t.Fatalf("reason %q must carry what the operator typed", got.HookSpecificOutput.PermissionDecisionReason)
	}
}
