package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/control"
)

// SessionVar is re-exported for callers already holding this package. See
// control.SessionVar for why it is declared over there.
const SessionVar = control.SessionVar

const hookEvent = "PreToolUse"

type hookInput struct {
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
}

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
// target names the command socket ask dials, carried here only so the note
// written when nothing is listening names the path that was tried. It is a
// function and not a string because resolving that path can cost a read of the
// daemon's state file, and this runs once per tool call: the payloads answered
// below without asking anybody must not pay for it.
//
// ctx is handed to ask untouched, with no deadline of its own. `approve ask`
// legitimately blocks for as long as the daemon's approval wait while a human
// decides, so a timeout here would allow every call the operator was too slow
// to answer, which is the one outcome this feature exists to prevent.
func RunPermissionHook(ctx context.Context, stdin io.Reader, out, errOut io.Writer, target func() string, ask hookAsker) {
	var payload hookInput
	raw, err := io.ReadAll(io.LimitReader(stdin, maxHookInput))
	if err == nil {
		err = json.Unmarshal(raw, &payload)
	}
	switch {
	case err != nil:
		logWriteFailure(fmt.Fprintf(errOut, "herrscher: could not read the hook payload, allowing: %v\n", err))
		writeHookDecision(out, errOut, "allow", "")
		return
	case payload.HookEventName != "" && payload.HookEventName != hookEvent:
		logWriteFailure(fmt.Fprintf(errOut, "herrscher: this hook answers %s, not %q, allowing\n", hookEvent, payload.HookEventName))
		writeHookDecision(out, errOut, "allow", "")
		return
	case payload.ToolName == "":
		logWriteFailure(fmt.Fprintln(errOut, "herrscher: the hook payload names no tool, allowing"))
		writeHookDecision(out, errOut, "allow", "")
		return
	}

	session := strings.TrimSpace(os.Getenv(SessionVar))
	if session == "" {
		logWriteFailure(fmt.Fprintf(errOut, "herrscher: no %s in the environment, allowing\n", SessionVar))
		writeHookDecision(out, errOut, "allow", "")
		return
	}

	argv := append([]string{nsApprove, "ask"}, cli.FlagArg(paramSession, session)...)
	argv = append(argv, cli.FlagArg("tool", payload.ToolName)...)
	argv = append(argv, cli.FlagArg("subject", approval.SubjectOf(payload.ToolName, payload.ToolInput))...)
	answer, handled, err := ask(ctx, argv)
	switch {
	case !handled:
		logWriteFailure(fmt.Fprintf(errOut, "herrscher: no daemon is listening on %s, allowing\n", target()))
		writeHookDecision(out, errOut, "allow", "")
		return
	case err != nil:
		logWriteFailure(fmt.Fprintf(errOut, "herrscher: the daemon could not answer, allowing: %v\n", err))
		writeHookDecision(out, errOut, "allow", "")
		return
	}

	var verdict struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(answer), &verdict); err != nil || verdict.Decision == "" {
		logWriteFailure(fmt.Fprintf(errOut, "herrscher: could not read the daemon's answer %q, allowing\n", answer))
		writeHookDecision(out, errOut, "allow", "")
		return
	}
	writeHookDecision(out, errOut, verdict.Decision, verdict.Reason)
}

// RunApprovalHook is RunPermissionHook wired to the daemon, which is what every
// real hook process wants. The command socket is where a short-lived process
// reaches it, locally or through the one ssh forwarded back; the same resolver
// serves what is dialled and what the note written when nothing answers names,
// so the two can never disagree.
//
// The resolution is deferred and memoized. It runs once per tool call across
// the whole fleet, and naming the instance means parsing the daemon's state
// file, which that daemon is meanwhile writing. A session started by this
// daemon carries the forwarded socket in its environment, so the common case
// answers from there and never touches state at all.
//
// It is called from the binary's own case for `approve hook`, which answers
// before the operator registry is built: building that registry can fail for
// reasons that have nothing to do with the tool call, and the hook must never
// fail.
func RunApprovalHook(ctx context.Context, stdin io.Reader, out, errOut io.Writer) {
	target := sync.OnceValue(func() string {
		return commandSocketTargetFrom(func() string { return ServedInstanceID(DefaultStatePath(), "") })
	})
	RunPermissionHook(ctx, stdin, out, errOut, target, func(c context.Context, argv []string) (string, bool, error) {
		return dispatchLiveCommand(c, target(), argv)
	})
}

func writeHookDecision(out, errOut io.Writer, decision, reason string) {
	type specific struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	}
	payload, err := json.Marshal(struct {
		HookSpecificOutput specific `json:"hookSpecificOutput"`
	}{specific{HookEventName: hookEvent, PermissionDecision: decision, PermissionDecisionReason: reason}})
	if err != nil {
		logWriteFailure(fmt.Fprintf(errOut, "herrscher: could not write the %s decision, allowing: %v\n", decision, err))
		return
	}
	logWriteFailure(fmt.Fprintf(out, "%s\n", payload))
}
