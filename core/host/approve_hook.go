package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/language"
)

// SessionVar is re-exported for callers already holding this package. See
// control.SessionVar for why it is declared over there.
const SessionVar = control.SessionVar

const hookEvent = "PreToolUse"

const (
	hookNoSession          = "hook.no_session"
	hookPayloadUnreadable  = "hook.payload_unreadable"
	hookNamesNoTool        = "hook.names_no_tool"
	hookNoDaemon           = "hook.no_daemon"
	hookCancelled          = "hook.cancelled"
	hookDaemonFailed       = "hook.daemon_failed"
	hookAnswerUnreadable   = "hook.answer_unreadable"
	hookOtherEvent         = "hook.other_event"
	hookDecisionUnwritable = "hook.decision_unwritable"
)

const (
	variableParam = "Variable"
	socketParam   = "Socket"
	expectedParam = "Expected"
	eventParam    = "Event"
	answerParam   = "Answer"
	decisionParam = "Decision"
)

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
// Every failure denies, and the text written to errOut is also the reason the
// model reads: a tool call that cannot be judged does not run. The one allow is
// a payload for another event, which is not a tool call and gives the daemon
// nothing to decide.
//
// target names the command socket ask dials, carried here only so the refusal
// written when nothing is listening names the path that was tried. It is a
// function and not a string because resolving that path can cost a read of the
// daemon's state file, and this runs once per tool call: the payloads answered
// below without asking anybody must not pay for it.
//
// ctx is handed to ask untouched, with no deadline of its own. `approve ask`
// legitimately blocks for as long as the daemon's approval wait while a human
// decides, so a timeout here would refuse every call the operator was too slow
// to answer, which is the outcome an approval exists to give a human the chance
// to change.
func RunPermissionHook(ctx context.Context, stdin io.Reader, out, errOut io.Writer, target func() string, ask hookAsker) {
	lang := language.Client(slog.Default(), "")
	var payload hookInput
	raw, err := io.ReadAll(io.LimitReader(stdin, maxHookInput))
	if err == nil {
		err = json.Unmarshal(raw, &payload)
	}
	switch {
	case err != nil:
		refuseHook(out, errOut, lang, hookPayloadUnreadable, map[string]any{detailParam: err.Error()})
		return
	case payload.HookEventName != "" && payload.HookEventName != hookEvent:
		note := hookText(lang, hookOtherEvent, map[string]any{expectedParam: hookEvent, eventParam: payload.HookEventName})
		logWriteFailure(fmt.Fprintln(errOut, note))
		writeHookDecision(out, errOut, lang, "allow", "")
		return
	case payload.ToolName == "":
		refuseHook(out, errOut, lang, hookNamesNoTool, nil)
		return
	}

	session := strings.TrimSpace(os.Getenv(SessionVar))
	if session == "" {
		refuseHook(out, errOut, lang, hookNoSession, map[string]any{variableParam: SessionVar})
		return
	}

	argv := append([]string{nsApprove, "ask"}, cli.FlagArg(paramSession, session)...)
	argv = append(argv, cli.FlagArg("tool", payload.ToolName)...)
	argv = append(argv, cli.FlagArg("subject", approval.SubjectOf(payload.ToolName, payload.ToolInput))...)
	answer, handled, err := ask(ctx, argv)
	switch {
	case !handled:
		refuseHook(out, errOut, lang, hookNoDaemon, map[string]any{socketParam: target()})
		return
	case err != nil && ctx.Err() != nil:
		refuseHook(out, errOut, lang, hookCancelled, nil)
		return
	case err != nil:
		refuseHook(out, errOut, lang, hookDaemonFailed, map[string]any{detailParam: err.Error()})
		return
	}

	var verdict struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(answer), &verdict); err != nil || verdict.Decision == "" {
		refuseHook(out, errOut, lang, hookAnswerUnreadable, map[string]any{answerParam: answer})
		return
	}
	writeHookDecision(out, errOut, lang, verdict.Decision, verdict.Reason)
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
func RunApprovalHook(ctx context.Context, layout RuntimeLayout, stdin io.Reader, out, errOut io.Writer) {
	target := sync.OnceValue(func() string {
		return commandSocketTargetFrom(layout, func() string { return ServedInstanceID(DefaultStatePath(), "") })
	})
	RunPermissionHook(ctx, stdin, out, errOut, target, func(c context.Context, argv []string) (string, bool, error) {
		return dispatchToOperatorDaemon(c, layout, target(), argv)
	})
}

func refuseHook(out, errOut io.Writer, lang i18n.Language, key string, params map[string]any) {
	reason := hookText(lang, key, params)
	logWriteFailure(fmt.Fprintln(errOut, reason))
	writeHookDecision(out, errOut, lang, "deny", reason)
}

func hookText(lang i18n.Language, key string, params map[string]any) string {
	return newHostMessages().text(lang, key, params)
}

func writeHookDecision(out, errOut io.Writer, lang i18n.Language, decision, reason string) {
	type specific struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	}
	payload, err := json.Marshal(struct {
		HookSpecificOutput specific `json:"hookSpecificOutput"`
	}{specific{HookEventName: hookEvent, PermissionDecision: decision, PermissionDecisionReason: reason}})
	if err != nil {
		logWriteFailure(fmt.Fprintln(errOut, hookText(lang, hookDecisionUnwritable, map[string]any{decisionParam: decision, detailParam: err.Error()})))
		return
	}
	logWriteFailure(fmt.Fprintf(out, "%s\n", payload))
}
