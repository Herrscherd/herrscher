package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/control"
)

// SessionVar is re-exported for callers already holding this package. It is
// declared in control, next to the command-socket variable, because the
// supervisor sets it and cannot import this package: core/host imports the
// supervisor, not the other way round.
const SessionVar = control.SessionVar

// hookEvent is the one Claude Code event this hook answers. It is both what we
// refuse to answer for anything else and what we echo back in the decision.
const hookEvent = "PreToolUse"

// hookInput is the part of Claude Code's PreToolUse payload we read. The rest
// of the payload is ignored on purpose: a field we do not use is a field whose
// renaming cannot break us.
type hookInput struct {
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
}

// hookAsker runs one argv against the daemon. handled is false when nothing was
// listening, which is a different answer from an error.
type hookAsker func(ctx context.Context, argv []string) (string, bool, error)

// RunPermissionHook is the whole hook process: read the vendor's payload, ask
// the daemon, write the vendor's answer. It never fails and never reports a
// failure to its caller: a non-zero exit is read by Claude Code as the hook
// crashing, which is not what we mean by any of our outcomes.
//
// Every failure allows, with a note on errOut naming what went wrong. A hook
// that refuses when it cannot reach the daemon would break `claude` run by hand
// in a materialized worktree, where no daemon watches anything. This is a
// guardrail against mistakes, not a sandbox.
//
// target is the command socket ask dials, carried here only so the note written
// when nothing is listening names the path that was tried.
//
// ctx is handed to ask untouched, with no deadline of its own. `approve ask`
// legitimately blocks for as long as the daemon's approval wait while a human
// decides, so a timeout here would allow every call the operator was too slow
// to answer, which is the one outcome this feature exists to prevent.
func RunPermissionHook(ctx context.Context, in io.Reader, out, errOut io.Writer, target string, ask hookAsker) {
	var payload hookInput
	raw, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err == nil {
		err = json.Unmarshal(raw, &payload)
	}
	switch {
	case err != nil:
		fmt.Fprintf(errOut, "herrscher: could not read the hook payload, allowing: %v\n", err)
		writeHookDecision(out, errOut, "allow", "")
		return
	case payload.HookEventName != "" && payload.HookEventName != hookEvent:
		// Only PreToolUse asks a question this hook can answer. Anything else is
		// a mis-edited settings file, and answering it as if it were a tool call
		// would put the wrong event name in the reply.
		fmt.Fprintf(errOut, "herrscher: this hook answers %s, not %q, allowing\n", hookEvent, payload.HookEventName)
		writeHookDecision(out, errOut, "allow", "")
		return
	case payload.ToolName == "":
		fmt.Fprintln(errOut, "herrscher: the hook payload names no tool, allowing")
		writeHookDecision(out, errOut, "allow", "")
		return
	}

	session := strings.TrimSpace(os.Getenv(SessionVar))
	if session == "" {
		fmt.Fprintf(errOut, "herrscher: no %s in the environment, allowing\n", SessionVar)
		writeHookDecision(out, errOut, "allow", "")
		return
	}

	// cli.FlagArg rather than a hand-written pair: the subject is free text taken
	// from the tool call (a command, a url, a query), so it can begin with "--",
	// which the daemon's parser refuses as a value in the two-token form. The
	// tool name and the session go through it for the same reason, both being
	// strings this process was handed rather than ones it chose.
	argv := append([]string{"approve", "ask"}, cli.FlagArg("session", session)...)
	argv = append(argv, cli.FlagArg("tool", payload.ToolName)...)
	argv = append(argv, cli.FlagArg("subject", approval.SubjectOf(payload.ToolName, payload.ToolInput))...)
	answer, handled, err := ask(ctx, argv)
	switch {
	case !handled:
		fmt.Fprintf(errOut, "herrscher: no daemon is listening on %s, allowing\n", target)
		writeHookDecision(out, errOut, "allow", "")
		return
	case err != nil:
		fmt.Fprintf(errOut, "herrscher: the daemon could not answer, allowing: %v\n", err)
		writeHookDecision(out, errOut, "allow", "")
		return
	}

	var verdict struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(answer), &verdict); err != nil || verdict.Decision == "" {
		fmt.Fprintf(errOut, "herrscher: could not read the daemon's answer %q, allowing\n", answer)
		writeHookDecision(out, errOut, "allow", "")
		return
	}
	writeHookDecision(out, errOut, verdict.Decision, verdict.Reason)
}

// RunApprovalHook is RunPermissionHook wired to the daemon, which is what every
// real hook process wants. The command socket is where a short-lived process
// reaches it, locally or through the one ssh forwarded back; the path is
// resolved here so the same string is both dialled and named in the note
// written when nothing answers.
//
// It is one function rather than a closure written twice because it has two
// callers: the `approve hook` verb, and the binary's own case for that verb,
// which answers before the operator registry is built. The second exists
// because building that registry can fail for reasons that have nothing to do
// with the tool call, and the hook must never fail.
func RunApprovalHook(ctx context.Context, in io.Reader, out, errOut io.Writer) {
	target := commandSocketTarget(ServedInstanceID(DefaultStatePath(), ""))
	RunPermissionHook(ctx, in, out, errOut, target, func(c context.Context, argv []string) (string, bool, error) {
		return dispatchLiveCommand(c, target, argv)
	})
}

// writeHookDecision renders the one shape Claude Code reads back from a
// PreToolUse hook.
func writeHookDecision(out, errOut io.Writer, decision, reason string) {
	type specific struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	}
	b, err := json.Marshal(struct {
		HookSpecificOutput specific `json:"hookSpecificOutput"`
	}{specific{HookEventName: hookEvent, PermissionDecision: decision, PermissionDecisionReason: reason}})
	if err != nil {
		// Three strings cannot fail to marshal, so this arm is unreachable today.
		// It is written anyway because staying silent on both streams is the one
		// outcome this hook must never have.
		fmt.Fprintf(errOut, "herrscher: could not write the %s decision, allowing: %v\n", decision, err)
		return
	}
	fmt.Fprintf(out, "%s\n", b)
}
