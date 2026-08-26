package host

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// testSocket is the path the hook is told it dialled. Nothing listens on it:
// the asker is a stub in every test here.
const testSocket = "/run/herrscher/test.sock"

// testTarget is that path as the hook takes it: a resolver it calls only when a
// note has to name the socket.
func testTarget() string { return testSocket }

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
	RunPermissionHook(context.Background(), in, &out, &errOut, testTarget, ask)
	want := []string{"approve", "ask", "--session", "s1", "--tool", "Bash", "--subject", "git push"}
	if strings.Join(seen, " ") != strings.Join(want, " ") {
		t.Fatalf("argv %v, want %v", seen, want)
	}
	d, reason := hookDecision(t, out.String())
	if d != "deny" || reason != "herrscher: no" {
		t.Fatalf("got %q/%q, want deny/herrscher: no", d, reason)
	}
}

// A subject the operator never chose: it comes from the tool call, so it can
// begin with "--". The daemon's parser refuses such a value in the two-token
// form, so the ask would error and the guardrail would fall open on a call it
// was written to stop.
func TestHookSpellsASubjectThatLooksLikeAFlag(t *testing.T) {
	t.Setenv(SessionVar, "s1")
	var seen []string
	ask := func(_ context.Context, argv []string) (string, bool, error) {
		seen = argv
		return `{"decision":"allow"}`, true, nil
	}
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"--force rm -rf /"}}`)
	var out, errOut bytes.Buffer
	RunPermissionHook(context.Background(), in, &out, &errOut, testTarget, ask)
	// Bounded before it is indexed: a regression that stops asking at all leaves
	// seen empty, and this test has to fail with its own message rather than
	// panic somewhere the message is lost.
	if len(seen) == 0 {
		t.Fatal("the daemon was never asked")
	}
	if want := "--subject=--force rm -rf /"; seen[len(seen)-1] != want {
		t.Fatalf("last argv token %q, want %q", seen[len(seen)-1], want)
	}
}

// The branch that turns the guardrail off. Without a session there is no policy
// to apply and nobody to ask, which is `claude` run by hand in a materialized
// worktree: it allows, and says on stderr which variable was missing, because a
// guardrail that is not in effect must never be silent about it.
func TestHookAllowsWithNoSessionInTheEnvironment(t *testing.T) {
	t.Setenv(SessionVar, "")
	ask := func(context.Context, []string) (string, bool, error) {
		t.Error("the daemon must not be asked about a call that belongs to no session")
		return "", false, nil
	}
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`)
	var out, errOut bytes.Buffer
	RunPermissionHook(context.Background(), in, &out, &errOut, testTarget, ask)
	if d, reason := hookDecision(t, out.String()); d != "allow" || reason != "" {
		t.Fatalf("got %q/%q, want a bare allow", d, reason)
	}
	if !strings.Contains(errOut.String(), SessionVar) {
		t.Fatalf("stderr was %q, want the variable whose absence turned the hook off", errOut.String())
	}
}

// The socket is resolved through a function so the payloads answered without
// asking anybody never pay for it: naming it reads the daemon's state file, and
// this runs once per tool call across the whole fleet.
func TestHookDoesNotResolveTheSocketItNeverDials(t *testing.T) {
	t.Setenv(SessionVar, "s1")
	resolved := 0
	target := func() string {
		resolved++
		return testSocket
	}
	ask := func(context.Context, []string) (string, bool, error) {
		t.Error("a payload for another event asks nobody")
		return "", false, nil
	}
	var out, errOut bytes.Buffer
	in := strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"Bash"}`)
	RunPermissionHook(context.Background(), in, &out, &errOut, target, ask)
	if resolved != 0 {
		t.Fatalf("the socket was resolved %d times, want none", resolved)
	}
}

func TestHookAllowsWhenNoDaemonListens(t *testing.T) {
	t.Setenv(SessionVar, "s1")
	ask := func(context.Context, []string) (string, bool, error) { return "", false, nil }
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`)
	var out, errOut bytes.Buffer
	RunPermissionHook(context.Background(), in, &out, &errOut, testTarget, ask)
	if d, _ := hookDecision(t, out.String()); d != "allow" {
		t.Fatalf("got %q, want allow: no daemon must not mean no claude", d)
	}
	// The note names the socket: an operator whose guardrail is silently not
	// running has one thing to check, and this is it.
	if !strings.Contains(errOut.String(), testSocket) {
		t.Fatalf("stderr was %q, want the socket it dialled", errOut.String())
	}
}

func TestHookAllowsOnGarbageInput(t *testing.T) {
	ask := func(context.Context, []string) (string, bool, error) {
		t.Fatal("the daemon must not be asked about input we could not read")
		return "", false, nil
	}
	var out, errOut bytes.Buffer
	RunPermissionHook(context.Background(), strings.NewReader("{not json"), &out, &errOut, testTarget, ask)
	if d, _ := hookDecision(t, out.String()); d != "allow" {
		t.Fatalf("got %q, want allow", d)
	}
	if !strings.Contains(errOut.String(), "could not read the hook payload") {
		t.Fatalf("stderr was %q, want the note naming the unreadable payload", errOut.String())
	}
}

// A payload that parsed but names no tool is a different mistake from one that
// did not parse, and the note must not blame the wrong one.
func TestHookAllowsWhenThePayloadNamesNoTool(t *testing.T) {
	ask := func(context.Context, []string) (string, bool, error) {
		t.Fatal("the daemon must not be asked about a payload with no tool")
		return "", false, nil
	}
	var out, errOut bytes.Buffer
	RunPermissionHook(context.Background(), strings.NewReader(`{"hook_event_name":"PreToolUse"}`), &out, &errOut, testTarget, ask)
	if d, _ := hookDecision(t, out.String()); d != "allow" {
		t.Fatalf("got %q, want allow", d)
	}
	if !strings.Contains(errOut.String(), "names no tool") {
		t.Fatalf("stderr was %q, want the note naming the missing tool", errOut.String())
	}
}

// Wired to another event by a mis-edited settings file, the hook allows with a
// note rather than answering a question it was not asked.
func TestHookAllowsOnAnotherEvent(t *testing.T) {
	t.Setenv(SessionVar, "s1")
	ask := func(context.Context, []string) (string, bool, error) {
		t.Fatal("only PreToolUse asks a question the daemon can answer")
		return "", false, nil
	}
	in := strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	var out, errOut bytes.Buffer
	RunPermissionHook(context.Background(), in, &out, &errOut, testTarget, ask)
	if d, _ := hookDecision(t, out.String()); d != "allow" {
		t.Fatalf("got %q, want allow", d)
	}
	if !strings.Contains(errOut.String(), "PostToolUse") {
		t.Fatalf("stderr was %q, want the event it was handed", errOut.String())
	}
}

func TestHookAllowsWhenTheDaemonErrors(t *testing.T) {
	t.Setenv(SessionVar, "s1")
	ask := func(context.Context, []string) (string, bool, error) {
		return "", true, context.DeadlineExceeded
	}
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`)
	var out, errOut bytes.Buffer
	RunPermissionHook(context.Background(), in, &out, &errOut, testTarget, ask)
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
	RunPermissionHook(context.Background(), in, &out, &errOut, testTarget, ask)
	if hadDeadline {
		t.Fatal("the hook put a deadline on the ask: it would cut short the wait on a human")
	}
}
