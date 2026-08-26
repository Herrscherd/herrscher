package host

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func hookDecision(t *testing.T, out string) (string, string) {
	t.Helper()
	var got struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("hook wrote unparseable json %q: %v", out, err)
	}
	if got.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Fatalf("hookEventName %q", got.HookSpecificOutput.HookEventName)
	}
	return got.HookSpecificOutput.PermissionDecision, got.HookSpecificOutput.PermissionDecisionReason
}

func TestHookAsksTheDaemonWithTheRightArgv(t *testing.T) {
	t.Setenv(SessionVar, "s1")
	var seen []string
	ask := func(_ context.Context, argv []string) (string, bool, error) {
		seen = argv
		return `{"decision":"deny","reason":"herrscher: no"}`, true, nil
	}
	in := strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git push"}}`)
	var out, errOut bytes.Buffer
	if code := RunPermissionHook(context.Background(), in, &out, &errOut, ask); code != 0 {
		t.Fatalf("exit %d, want 0: a hook that exits non-zero is read as a crash", code)
	}
	want := []string{"approve", "ask", "--session", "s1", "--tool", "Bash", "--subject", "git push"}
	if strings.Join(seen, " ") != strings.Join(want, " ") {
		t.Fatalf("argv %v, want %v", seen, want)
	}
	d, reason := hookDecision(t, out.String())
	if d != "deny" || reason != "herrscher: no" {
		t.Fatalf("got %q/%q, want deny/herrscher: no", d, reason)
	}
}

func TestHookAllowsWhenNoDaemonListens(t *testing.T) {
	t.Setenv(SessionVar, "s1")
	ask := func(context.Context, []string) (string, bool, error) { return "", false, nil }
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`)
	var out, errOut bytes.Buffer
	if code := RunPermissionHook(context.Background(), in, &out, &errOut, ask); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if d, _ := hookDecision(t, out.String()); d != "allow" {
		t.Fatalf("got %q, want allow: no daemon must not mean no claude", d)
	}
	if errOut.Len() == 0 {
		t.Fatal("allowing by default must say so on stderr")
	}
}

func TestHookAllowsOnGarbageInput(t *testing.T) {
	ask := func(context.Context, []string) (string, bool, error) {
		t.Fatal("the daemon must not be asked about input we could not read")
		return "", false, nil
	}
	var out, errOut bytes.Buffer
	if code := RunPermissionHook(context.Background(), strings.NewReader("{not json"), &out, &errOut, ask); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if d, _ := hookDecision(t, out.String()); d != "allow" {
		t.Fatalf("got %q, want allow", d)
	}
}

func TestHookAllowsWhenTheDaemonErrors(t *testing.T) {
	t.Setenv(SessionVar, "s1")
	ask := func(context.Context, []string) (string, bool, error) {
		return "", true, context.DeadlineExceeded
	}
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`)
	var out, errOut bytes.Buffer
	RunPermissionHook(context.Background(), in, &out, &errOut, ask)
	if d, _ := hookDecision(t, out.String()); d != "allow" {
		t.Fatalf("got %q, want allow", d)
	}
}

// TestHookImposesNoDeadlineOfItsOwn is the whole point of the feature, asserted:
// `approve ask` blocks while a human decides, and a hook that gave up first
// would allow every call an operator was too slow to answer.
func TestHookImposesNoDeadlineOfItsOwn(t *testing.T) {
	t.Setenv(SessionVar, "s1")
	hadDeadline := true
	ask := func(ctx context.Context, _ []string) (string, bool, error) {
		_, hadDeadline = ctx.Deadline()
		return `{"decision":"allow"}`, true, nil
	}
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`)
	var out, errOut bytes.Buffer
	RunPermissionHook(context.Background(), in, &out, &errOut, ask)
	if hadDeadline {
		t.Fatal("the hook put a deadline on the ask: it would cut short the wait on a human")
	}
}
