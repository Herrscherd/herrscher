package host

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func hookSession(t *testing.T) {
	t.Helper()
	t.Setenv(SessionVar, "s1")
	t.Setenv(TokenVar, "token-s1")
}

func TestHookSendsTheSessionTokenWithTheAsk(t *testing.T) {
	pinEnglish(t)
	hookSession(t)
	var presented string
	ask := func(ctx context.Context, _ []string) (string, bool, error) {
		presented = callerTokenFrom(ctx)
		return `{"decision":"allow"}`, true, nil
	}
	in := strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, in, &out, &errOut, testTarget, ask)
	if presented != "token-s1" {
		t.Fatalf("the ask carried token %q, want the session's own", presented)
	}
}

func TestHookRefusesWithoutASessionToken(t *testing.T) {
	pinEnglish(t)
	t.Setenv(SessionVar, "s1")
	t.Setenv(TokenVar, "")
	asked := false
	ask := func(context.Context, []string) (string, bool, error) {
		asked = true
		return `{"decision":"allow"}`, true, nil
	}
	in := strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, in, &out, &errOut, testTarget, ask)
	if asked {
		t.Fatal("the hook asked the daemon without a token")
	}
	if d, _ := hookDecision(t, out.String()); d != "deny" {
		t.Fatalf("decision %q, want deny", d)
	}
}
