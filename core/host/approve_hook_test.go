package host

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const testSocket = "/run/herrscher/test.sock"

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
	if len(seen) == 0 {
		t.Fatal("the daemon was never asked")
	}
	if want := "--subject=--force rm -rf /"; seen[len(seen)-1] != want {
		t.Fatalf("last argv token %q, want %q", seen[len(seen)-1], want)
	}
}

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

func TestHookNamesACancelledApprovalApartFromAnUnansweredOne(t *testing.T) {
	t.Setenv(SessionVar, "s1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ask := func(c context.Context, _ []string) (string, bool, error) {
		return "", true, c.Err()
	}
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`)
	var out, errOut bytes.Buffer
	RunPermissionHook(ctx, in, &out, &errOut, testTarget, ask)
	if d, _ := hookDecision(t, out.String()); d != "allow" {
		t.Fatalf("got %q, want allow: a cancelled approval must not block the call", d)
	}
	msg := errOut.String()
	if !strings.Contains(msg, "cancelled") {
		t.Fatalf("stderr was %q, want it to say the approval was cancelled", msg)
	}
	if strings.Contains(msg, "could not answer") || strings.Contains(msg, "no daemon is listening") {
		t.Fatalf("stderr was %q, want no daemon-absent or could-not-answer wording", msg)
	}
}

func TestHookAppliesAnAnswerThatArrivesAfterCancellation(t *testing.T) {
	for _, decision := range []string{"deny", "allow"} {
		t.Run(decision, func(t *testing.T) {
			t.Setenv(SessionVar, "s1")
			ctx, cancel := context.WithCancel(context.Background())
			ask := func(context.Context, []string) (string, bool, error) {
				cancel()
				return `{"decision":"` + decision + `","reason":"herrscher: answered"}`, true, nil
			}
			in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"git push"}}`)
			var out, errOut bytes.Buffer
			RunPermissionHook(ctx, in, &out, &errOut, testTarget, ask)
			d, reason := hookDecision(t, out.String())
			if d != decision || reason != "herrscher: answered" {
				t.Fatalf("got %q/%q, want %q: a delivered answer must not be replaced by the cancellation", d, reason, decision)
			}
		})
	}
}
