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
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestHookThroughTheCommandSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	approvalTab := newApprovalTable()
	menus := &menuRecorder{menus: make(chan []contracts.Choice, 4)}
	reg := newApprovalRegistry(t, approvalTab, menus)
	sock := startApprovalSocket(ctx, t, reg)

	t.Setenv(SessionVar, "s1")
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"git push origin master"}}`)
	var out, errOut bytes.Buffer
	done := runHookAsync(ctx, in, &out, &errOut, sock)

	id := awaitPendingApproval(t, menus.menus)
	if _, err := reg.Dispatch(ctx, []string{"approve", "deny", id, "--reason", "not today"}); err != nil {
		t.Fatalf("deny: %v", err)
	}
	<-done
	assertHookDeniedWithReason(t, out.Bytes(), "not today")
}

type menuRecorder struct {
	fanRecorder

	menus chan []contracts.Choice
}

func (m *menuRecorder) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: "rec", Capabilities: contracts.Capabilities{SelectMenus: true}}
}

func (m *menuRecorder) Menu(_ context.Context, _ contracts.Conversation, _ contracts.MessageID, _ string, choices []contracts.Choice) error {
	m.menus <- choices
	return nil
}

func newApprovalRegistry(t *testing.T, approvalTab *approvalTable, menus *menuRecorder) *cli.Registry {
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
	sessionTab := newSessionTable()
	sessionTab.register("s1", newSessionDriver("s1", []contracts.GatewaySet{{Gateway: menus, Reader: menus}}, nil, nil))
	reg := &cli.Registry{}
	if err := addApproveCommands(reg, fixedLanguage(i18n.English), st, nil, approvalTab, sessionTab); err != nil {
		t.Fatalf("addApproveCommands: %v", err)
	}
	return reg
}

func startApprovalSocket(ctx context.Context, t *testing.T, reg *cli.Registry) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "cmd.sock")
	serveCommandSocket(ctx, sock, textDispatch(reg))
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

func awaitPendingApproval(t *testing.T, menus <-chan []contracts.Choice) string {
	t.Helper()
	select {
	case choices := <-menus:
		id, _, ok := strings.Cut(strings.TrimPrefix(choices[0].Value, approvalPickPrefix), ":")
		if !ok {
			t.Fatalf("approval menu value %q carries no id", choices[0].Value)
		}
		return id
	case <-time.After(5 * time.Second):
		t.Fatal("the hook never reached the operator's menu")
	}
	return ""
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
