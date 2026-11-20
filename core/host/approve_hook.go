package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/approvalsock"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/language"
)

// SessionVar is re-exported for callers already holding this package.
const SessionVar = control.SessionVar

const hookEvent = "PreToolUse"

const (
	hookNoSession            = "hook.no_session"
	hookNoToken              = "hook.no_token"
	hookPayloadUnreadable    = "hook.payload_unreadable"
	hookPayloadTooLarge      = "hook.payload_too_large"
	hookNamesNoTool          = "hook.names_no_tool"
	hookNoDaemon             = "hook.no_daemon"
	hookCancelled            = "hook.cancelled"
	hookDaemonFailed         = "hook.daemon_failed"
	hookAnswerUnreadable     = "hook.answer_unreadable"
	hookOtherEvent           = "hook.other_event"
	hookDecisionUnwritable   = "hook.decision_unwritable"
	hookApprovalSocketFailed = "hook.approval_socket_failed"
	hookApprovalTimeout      = "hook.approval_timeout"
)

const (
	variableParam = "Variable"
	socketParam   = "Socket"
	expectedParam = "Expected"
	eventParam    = "Event"
	answerParam   = "Answer"
	decisionParam = "Decision"
	waitParam     = "Wait"
	boundParam    = "Bound"
)

const (
	hookAnswerMargin   = 30 * time.Second
	bridgeAnswerMargin = 60 * time.Second
)

func hookAnswerWait() time.Duration {
	return agent.HookWait - hookAnswerMargin
}

// BridgeApprovalWait is how long the bridge waits for an approval answer: below the hook's own bound, so the refusal the operator sees is the bridge's.
func BridgeApprovalWait() time.Duration {
	return agent.HookWait - bridgeAnswerMargin
}

const modelParam = "Model"

type hookInput struct {
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
}

type hookAsker func(ctx context.Context, argv []string) (string, bool, error)

// RunPermissionHook answers one tool-call payload from the daemon and fails closed on every error.
func RunPermissionHook(ctx context.Context, owner *Messages, stdin io.Reader, out, errOut io.Writer, target func() string, answerWait time.Duration, ask hookAsker) error {
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

	if socket := strings.TrimSpace(os.Getenv(approvalsock.EnvApprovalSocket)); socket != "" {
		return askApprovalSocket(ctx, messages, out, errOut, lang, socket, answerWait, payload)
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
	bounded, cancel := context.WithTimeout(ctx, answerWait)
	defer cancel()
	answer, handled, err := ask(withCallerToken(bounded, token), argv)
	switch {
	case !handled:
		return refuseHook(messages, out, errOut, lang, hookNoDaemon, map[string]any{socketParam: target()})
	case err != nil && answerTimedOut(ctx, bounded):
		return refuseHook(messages, out, errOut, lang, hookApprovalTimeout, map[string]any{waitParam: answerWait.String()})
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

func askApprovalSocket(ctx context.Context, messages *hostMessages, out, errOut io.Writer, lang i18n.Language, socket string, answerWait time.Duration, payload hookInput) error {
	req := contracts.ApprovalRequest{
		ID:      newApprovalID(),
		Tool:    payload.ToolName,
		Subject: approval.SubjectOf(payload.ToolName, payload.ToolInput),
	}
	bounded, cancel := context.WithTimeout(ctx, answerWait)
	defer cancel()
	answer, err := approvalsock.Ask(bounded, socket, req)
	if err != nil && answerTimedOut(ctx, bounded) {
		return refuseHook(messages, out, errOut, lang, hookApprovalTimeout, map[string]any{waitParam: answerWait.String()})
	}
	if err != nil {
		return refuseHook(messages, out, errOut, lang, hookApprovalSocketFailed, map[string]any{detailParam: err.Error()})
	}
	if !answer.Allowed() {
		return writeHookDecision(messages, out, errOut, lang, "deny", answer.Reason)
	}
	return writeHookDecision(messages, out, errOut, lang, "allow", answer.Reason)
}

// RunApprovalHook is RunPermissionHook wired to the operator daemon.
func RunApprovalHook(ctx context.Context, layout RuntimeLayout, owner *Messages, stdin io.Reader, out, errOut io.Writer) error {
	target := sync.OnceValue(func() string {
		return commandSocketTargetFrom(layout, func() string { return ServedInstanceID(DefaultStatePath(), "") })
	})
	return RunPermissionHook(ctx, owner, stdin, out, errOut, target, hookAnswerWait(), func(c context.Context, argv []string) (string, bool, error) {
		return dispatchToOperatorDaemon(c, owner.owner, layout, target(), argv)
	})
}

func answerTimedOut(parent, bounded context.Context) bool {
	return parent.Err() == nil && errors.Is(bounded.Err(), context.DeadlineExceeded)
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
