package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/control"
)

// SessionVar is re-exported for callers already holding this package. It is
// declared in control, next to the command-socket variable, because the
// supervisor sets it and cannot import this package: core/host imports the
// supervisor, not the other way round.
const SessionVar = control.SessionVar

// hookInput is the part of Claude Code's PreToolUse payload we read. The rest
// of the payload is ignored on purpose: a field we do not use is a field whose
// renaming cannot break us.
type hookInput struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
}

// hookAsker runs one argv against the daemon. handled is false when nothing was
// listening, which is a different answer from an error.
type hookAsker func(ctx context.Context, argv []string) (string, bool, error)

// RunPermissionHook is the whole hook process: read the vendor's payload, ask
// the daemon, write the vendor's answer. It returns the process exit code, and
// that code is always 0: a non-zero exit is read by Claude Code as the hook
// crashing, which is not what we mean by any of our outcomes.
//
// Every failure allows. A hook that refuses when it cannot reach the daemon
// would break `claude` run by hand in a materialized worktree, where no daemon
// watches anything. This is a guardrail against mistakes, not a sandbox.
//
// ctx is handed to ask untouched, with no deadline of its own. `approve ask`
// legitimately blocks for as long as the daemon's approval wait while a human
// decides, so a timeout here would allow every call the operator was too slow
// to answer, which is the one outcome this feature exists to prevent.
func RunPermissionHook(ctx context.Context, in io.Reader, out, errOut io.Writer, ask hookAsker) int {
	var payload hookInput
	raw, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err == nil {
		err = json.Unmarshal(raw, &payload)
	}
	if err != nil || payload.ToolName == "" {
		fmt.Fprintf(errOut, "herrscher: could not read the hook payload, allowing: %v\n", err)
		return writeHookDecision(out, "allow", "")
	}

	session := strings.TrimSpace(os.Getenv(SessionVar))
	if session == "" {
		fmt.Fprintf(errOut, "herrscher: no %s in the environment, allowing\n", SessionVar)
		return writeHookDecision(out, "allow", "")
	}

	argv := []string{"approve", "ask", "--session", session, "--tool", payload.ToolName,
		"--subject", approval.SubjectOf(payload.ToolName, payload.ToolInput)}
	answer, handled, err := ask(ctx, argv)
	switch {
	case !handled:
		fmt.Fprintln(errOut, "herrscher: no daemon is listening, allowing")
		return writeHookDecision(out, "allow", "")
	case err != nil:
		fmt.Fprintf(errOut, "herrscher: the daemon could not answer, allowing: %v\n", err)
		return writeHookDecision(out, "allow", "")
	}

	var verdict struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(answer), &verdict); err != nil || verdict.Decision == "" {
		fmt.Fprintf(errOut, "herrscher: could not read the daemon's answer %q, allowing\n", answer)
		return writeHookDecision(out, "allow", "")
	}
	return writeHookDecision(out, verdict.Decision, verdict.Reason)
}

// RunApprovalHook is RunPermissionHook wired to the daemon, which is what every
// real hook process wants. ForwardToDaemon is how a short-lived process reaches
// the command socket, locally or through the one ssh forwarded back.
//
// It is one function rather than a closure written twice because it has two
// callers: the `approve hook` verb, and the binary's own case for that verb,
// which answers before the operator registry is built. The second exists
// because building that registry can fail for reasons that have nothing to do
// with the tool call, and the hook must never fail.
func RunApprovalHook(ctx context.Context, in io.Reader, out, errOut io.Writer) int {
	return RunPermissionHook(ctx, in, out, errOut, func(c context.Context, argv []string) (string, bool, error) {
		return ForwardToDaemon(c, DefaultStatePath(), "", argv)
	})
}

// writeHookDecision renders the one shape Claude Code reads back from a
// PreToolUse hook.
func writeHookDecision(out io.Writer, decision, reason string) int {
	type specific struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	}
	b, err := json.Marshal(struct {
		HookSpecificOutput specific `json:"hookSpecificOutput"`
	}{specific{HookEventName: "PreToolUse", PermissionDecision: decision, PermissionDecisionReason: reason}})
	if err != nil {
		return 0
	}
	fmt.Fprintf(out, "%s\n", b)
	return 0
}
