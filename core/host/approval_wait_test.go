package host

import (
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/approvalsock"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func TestApprovalWaitsKeepTheBridgeBelowTheHookBelowTheCLIWait(t *testing.T) {
	bridge, hook, cli := BridgeApprovalWait(), hookAnswerWait(), agent.HookWait
	if bridge >= hook || hook >= cli {
		t.Fatalf("bridge %s, hook %s, CLI %s: want them in that order so the operator sees the bridge's refusal", bridge, hook, cli)
	}
}

func silentApprovalSocket(t *testing.T) string {
	t.Helper()
	path := privateSocketPath(t)
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			mu.Lock()
			held = append(held, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close listener: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range held {
			if err := conn.Close(); err != nil {
				t.Errorf("close held connection: %v", err)
			}
		}
	})
	return path
}

func TestHookRefusesWhenTheApprovalSocketNeverAnswers(t *testing.T) {
	pinEnglish(t)
	t.Setenv(SessionVar, "")
	t.Setenv(TokenVar, "")
	t.Setenv(approvalsock.EnvApprovalSocket, silentApprovalSocket(t))
	var out, errOut bytes.Buffer
	started := time.Now()
	if err := RunPermissionHook(context.Background(), NewMessages(), strings.NewReader(bashPayload), &out, &errOut, testTarget, 50*time.Millisecond, forbidDaemon(t)); err != nil {
		t.Fatalf("the hook could not deliver its decision: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("the hook waited %s for a socket that never answers", elapsed)
	}
	d, reason := hookDecision(t, out.String())
	if d != "deny" || !strings.Contains(reason, "50ms") {
		t.Fatalf("got %q/%q, want a denial naming the 50ms bound", d, reason)
	}
}

func TestHookRefusesWhenTheDaemonNeverAnswers(t *testing.T) {
	pinEnglish(t)
	hookSession(t)
	t.Setenv(approvalsock.EnvApprovalSocket, "")
	ask := func(ctx context.Context, _ []string) (string, bool, error) {
		<-ctx.Done()
		return "", true, ctx.Err()
	}
	var out, errOut bytes.Buffer
	if err := RunPermissionHook(context.Background(), NewMessages(), strings.NewReader(bashPayload), &out, &errOut, testTarget, 50*time.Millisecond, ask); err != nil {
		t.Fatalf("the hook could not deliver its decision: %v", err)
	}
	d, reason := hookDecision(t, out.String())
	if d != "deny" || !strings.Contains(reason, "50ms") {
		t.Fatalf("got %q/%q, want a denial naming the 50ms bound", d, reason)
	}
}
