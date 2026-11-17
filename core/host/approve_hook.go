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

// SessionVar is re-exported for callers already holding this package.
const SessionVar = control.SessionVar

const hookEvent = "PreToolUse"

const (
	hookNoSession          = "hook.no_session"
	hookNoToken            = "hook.no_token"
	hookPayloadUnreadable  = "hook.payload_unreadable"
	hookPayloadTooLarge    = "hook.payload_too_large"
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

const modelParam = "Model"

type hookInput struct {
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
}

type hookAsker func(ctx context.Context, argv []string) (string, bool, error)

// RunPermissionHook answers one tool-call payload from the daemon and fails closed on every error.
func RunPermissionHook(ctx context.Context, owner *Messages, stdin io.Reader, out, errOut io.Writer, target func() string, ask hookAsker) error {
	messages := owner.owner
	lang := language.Client(slog.Default(), "")
	var payload hookInput
	raw, err := io.ReadAll(io.LimitReader(stdin, maxHookInput+1))
	if err == nil && len(raw) > maxHookInput {
		return refuseHook(messages, out, errOut, lang, hookPayloadTooLarge, nil)
	}
	if err == nil {
		err = json.Unmarshal(raw, &payload)
	}
	switch {
	case err != nil:
		return refuseHook(messages, out, errOut, lang, hookPayloadUnreadable, map[string]any{detailParam: err.Error()})
	case payload.HookEventName != "" && payload.HookEventName != hookEvent:
		note := hookText(messages, lang, hookOtherEvent, map[string]any{expectedParam: hookEvent, eventParam: payload.HookEventName})
		logWriteFailure(fmt.Fprintln(errOut, note))
		return writeHookDecision(messages, out, errOut, lang, "allow", "")
	case payload.ToolName == "":
		return refuseHook(messages, out, errOut, lang, hookNamesNoTool, nil)
	}

	session := strings.TrimSpace(os.Getenv(SessionVar))
	if session == "" {
		return refuseHook(messages, out, errOut, lang, hookNoSession, map[string]any{variableParam: SessionVar})
	}
	token := os.Getenv(TokenVar)
	if token == "" {
		return refuseHook(messages, out, errOut, lang, hookNoToken, map[string]any{variableParam: TokenVar})
	}

	argv := append([]string{nsApprove, "ask"}, cli.FlagArg(paramSession, session)...)
	argv = append(argv, cli.FlagArg("tool", payload.ToolName)...)
	argv = append(argv, cli.FlagArg("subject", approval.SubjectOf(payload.ToolName, payload.ToolInput))...)
	answer, handled, err := ask(withCallerToken(ctx, token), argv)
	switch {
	case !handled:
		return refuseHook(messages, out, errOut, lang, hookNoDaemon, map[string]any{socketParam: target()})
	case err != nil && ctx.Err() != nil:
		return refuseHook(messages, out, errOut, lang, hookCancelled, nil)
	case err != nil:
		return refuseHook(messages, out, errOut, lang, hookDaemonFailed, map[string]any{detailParam: err.Error()})
	}

	var verdict struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(answer), &verdict); err != nil || !isDecision(verdict.Decision) {
		return refuseHook(messages, out, errOut, lang, hookAnswerUnreadable, map[string]any{answerParam: answer})
	}
	return writeHookDecision(messages, out, errOut, lang, verdict.Decision, verdict.Reason)
}

// RunApprovalHook is RunPermissionHook wired to the operator daemon.
func RunApprovalHook(ctx context.Context, layout RuntimeLayout, stdin io.Reader, out, errOut io.Writer) error {
	target := sync.OnceValue(func() string {
		return commandSocketTargetFrom(layout, func() string { return ServedInstanceID(DefaultStatePath(), "") })
	})
	owner := NewMessages()
	return RunPermissionHook(ctx, owner, stdin, out, errOut, target, func(c context.Context, argv []string) (string, bool, error) {
		return dispatchToOperatorDaemon(c, owner.owner, layout, target(), argv)
	})
}

func refuseHook(messages *hostMessages, out, errOut io.Writer, lang i18n.Language, key string, params map[string]any) error {
	reason := hookText(messages, lang, key, params)
	logWriteFailure(fmt.Fprintln(errOut, reason))
	return writeHookDecision(messages, out, errOut, lang, "deny", reason)
}

func hookText(messages *hostMessages, lang i18n.Language, key string, params map[string]any) string {
	return messages.text(lang, key, params)
}

func writeHookDecision(messages *hostMessages, out, errOut io.Writer, lang i18n.Language, decision, reason string) error {
	type specific struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	}
	payload, err := json.Marshal(struct {
		HookSpecificOutput specific `json:"hookSpecificOutput"`
	}{specific{HookEventName: hookEvent, PermissionDecision: decision, PermissionDecisionReason: reason}})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "%s\n", payload); err != nil {
		logWriteFailure(fmt.Fprintln(errOut, hookText(messages, lang, hookDecisionUnwritable, map[string]any{decisionParam: decision, detailParam: err.Error()})))
		return err
	}
	return nil
}

func isDecision(decision string) bool {
	return decision == "allow" || decision == "deny"
}
