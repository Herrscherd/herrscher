package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const approvalsFile = "APPROVALS"

var errScopeConflict = errors.New("--session and --always are two scopes: pick one")

func addApproveCommands(reg *cli.Registry, hostState *state.State, agents *agent.Store, approvals *approvalTable, sessions *sessionTable) error {
	return addVerbs(reg,
		approveListVerb(approvals),
		approveAllowVerb(approvals, sessions),
		approveDenyVerb(approvals, sessions),
		approveRuleVerb(hostState),
		approveAskVerb(hostState, agents, approvals, sessions),
		approveModeVerb(hostState),
	)
}

func approveListVerb(approvals *approvalTable) contracts.Cmd {
	return contracts.New(nsApprove, "list").
		Help("list the actions waiting for an answer").
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			return listApprovals(approvals, in.JSON)
		})
}

func listApprovals(approvals *approvalTable, asJSON bool) (string, error) {
	pending := pendingApprovals(approvals)
	sort.SliceStable(pending, func(left, job int) bool {
		if pending[left].Age != pending[job].Age {
			return pending[left].Age > pending[job].Age
		}
		return pending[left].ID < pending[job].ID
	})
	if asJSON {
		body, err := json.Marshal(pending)
		return string(body), err
	}
	if len(pending) == 0 {
		return "nothing is waiting", nil
	}
	var body strings.Builder
	for _, p := range pending {
		fmt.Fprintf(&body, "- %s  %s  %s(%s)  waiting %s\n", p.ID, p.Session, p.Tool, p.Subject, p.Age)
	}
	return strings.TrimRight(body.String(), "\n"), nil
}

func approveAllowVerb(approvals *approvalTable, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsApprove, "allow").
		Help("let a waiting action run; --session keeps it for this session, --always writes a rule for it").
		ValueParam("id", "request id; also takes a bare argument", false).
		Param(paramSession, "let the same action run for the rest of the session", false).
		Param("always", "write a rule that lets this exact action run from now on", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			approvalID, scope, err := allowOperands(in)
			if err != nil {
				return "", err
			}
			if !settleApproval(approvals, sessions, approvalID, approvalAnswer{scope: scope}) {
				return "", &requestNotWaitingError{ID: approvalID}
			}
			return "allowed " + approvalID, nil
		})
}

func allowOperands(in contracts.Input) (string, approvalScope, error) {
	operands := append([]string{}, in.Rest...)
	if id := in.Get("id"); id != "" {
		operands = append([]string{id}, operands...)
	}
	var scopes []approvalScope
	for _, flag := range []struct {
		name  string
		scope approvalScope
	}{{paramSession, scopeSession}, {"always", scopeAlways}} {
		value := in.Get(flag.name)
		if value == "" {
			continue
		}
		scopes = append(scopes, flag.scope)
		if value != "true" {
			operands = append(operands, value)
		}
	}
	if len(scopes) > 1 {
		return "", "", errScopeConflict
	}
	if len(operands) == 0 {
		return "", "", errRequestNameMissing
	}
	scope := scopeOnce
	if len(scopes) == 1 {
		scope = scopes[0]
	}
	return operands[0], scope, nil
}

func approveDenyVerb(approvals *approvalTable, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsApprove, "deny").
		Help("refuse a waiting action; the reason is what the agent reads").
		ValueParam("id", "request id; also takes a bare argument", false).
		ValueParam("reason", "why, in the agent's own transcript", false).
		Do(func(_ context.Context, input contracts.Input) (string, error) {
			approvalID := firstOf(input.Get("id"), input.Rest)
			if approvalID == "" {
				return "", errRequestNameMissing
			}
			answer := approvalAnswer{scope: scopeDeny, reason: operatorRefusal(input.Get("reason"))}
			if !settleApproval(approvals, sessions, approvalID, answer) {
				return "", &requestNotWaitingError{ID: approvalID}
			}
			return "denied " + approvalID, nil
		})
}

func approveRuleVerb(store *state.State) contracts.Cmd {
	return contracts.New(nsApprove, "rule").
		Help("add a rule, remove one with --rm, or list them with no argument").
		ValueParam("rule", "e.g. `ask Bash(git push*)`; also takes a bare argument", false).
		ValueParam("rm", "the rule to remove, written exactly as it is listed", false).
		Do(func(_ context.Context, input contracts.Input) (string, error) {
			if rm := strings.TrimSpace(input.Get("rm")); rm != "" {
				return removeApprovalRule(store, rm)
			}
			raw := firstOf(input.Get("rule"), input.Rest)
			if raw == "" {
				return listApprovalRules(store, input.JSON)
			}
			return addApprovalRule(store, raw)
		})
}

func removeApprovalRule(store *state.State, raw string) (string, error) {
	parsed, err := approval.ParseRule(raw)
	if err != nil {
		return "", err
	}
	removed, err := store.RemoveApprovalRule(parsed.String())
	if err != nil {
		return "", err
	}
	if !removed {
		return "", &noSuchRuleError{Rule: parsed.String()}
	}
	return "removed " + parsed.String(), nil
}

func listApprovalRules(st *state.State, asJSON bool) (string, error) {
	rules := st.ApprovalRules()
	if asJSON {
		if rules == nil {
			rules = []string{}
		}
		b, err := json.Marshal(rules)
		return string(b), err
	}
	if len(rules) == 0 {
		return "no rule: everything is allowed, which is how herrscher behaved before approvals existed", nil
	}
	return "- " + strings.Join(rules, "\n- "), nil
}

func addApprovalRule(store *state.State, raw string) (string, error) {
	parsed, err := approval.ParseRule(raw)
	if err != nil {
		return "", err
	}
	if err := store.AddApprovalRule(parsed.String()); err != nil {
		return "", err
	}
	return "added " + parsed.String(), nil
}

func approveAskVerb(store *state.State, agents *agent.Store, approvals *approvalTable, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsApprove, "ask").
		Help("machine verb: ask the daemon whether one tool call may run").
		ValueParam(paramSession, "the session the call belongs to", true).
		ValueParam("tool", "tool name, e.g. Bash", true).
		ValueParam("subject", "the part a rule matches: the command, the path, the url", false).
		Do(func(ctx context.Context, input contracts.Input) (string, error) {
			session := strings.TrimSpace(input.Get(paramSession))
			req := approval.Request{
				Tool:    strings.TrimSpace(input.Get("tool")),
				Subject: input.Get("subject"),
			}
			pol, mode := sessionPolicy(store, agents, session)
			decision, reason := askApproval(ctx, approvals, sessions, session, req, pol, mode, store.ApprovalWait())
			payload, err := json.Marshal(struct {
				Decision string `json:"decision"`
				Reason   string `json:"reason,omitempty"`
			}{Decision: string(decision), Reason: reason})
			return string(payload), err
		})
}

func approveModeVerb(store *state.State) contracts.Cmd {
	return contracts.New(nsApprove, "mode").
		Help("set a session's stance: ask (default), accept-edits, plan, auto, bypass, strict, or smart (a model rates each call)").
		ValueParam(paramSession, "session name; also takes a bare argument", false).
		ValueParam("mode", "ask | accept-edits | plan | auto | bypass | strict | smart; also takes a second bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			return setApprovalMode(store, in)
		})
}

func setApprovalMode(store *state.State, in contracts.Input) (string, error) {
	ops := parseModeOperands(in)
	name, mode := ops.session, ops.mode
	if name == "" || mode == "" {
		return "", errApproveModeUsage
	}
	switch approval.Mode(mode) {
	case approval.ModeAsk, approval.ModeAcceptEdits, approval.ModePlan, approval.ModeAuto, approval.ModeBypass, approval.ModeStrict, approval.ModeSmart:
	default:
		return "", &unknownModeError{Mode: mode}
	}
	if err := store.SetSessionApprovals(name, mode); err != nil {
		return "", err
	}
	if approval.Mode(mode) == approval.ModeSmart && !riskModelConfigured() {
		return fmt.Sprintf("%s is now %s, but no LLM backend is configured to rate the calls: until one is, smart decides as ask does, so a call no rule names runs unasked", name, mode), nil
	}
	if mode != string(approval.ModeBypass) {
		return fmt.Sprintf("%s is now %s (a session created with --approvals bypass has no hook: recreate it for this to bite)", name, mode), nil
	}
	return fmt.Sprintf("%s is now %s", name, mode), nil
}

type modeOperands struct {
	session string
	mode    string
}

func parseModeOperands(input contracts.Input) modeOperands {
	rest := input.Rest
	name := strings.TrimSpace(input.Get(paramSession))
	if name == "" && len(rest) > 0 {
		name, rest = strings.TrimSpace(rest[0]), rest[1:]
	}
	mode := strings.TrimSpace(input.Get("mode"))
	if mode == "" && len(rest) > 0 {
		mode = strings.TrimSpace(rest[len(rest)-1])
	}
	return modeOperands{session: name, mode: mode}
}

func firstOf(flag string, rest []string) string {
	if v := strings.TrimSpace(flag); v != "" {
		return v
	}
	if len(rest) > 0 {
		return strings.TrimSpace(rest[0])
	}
	return ""
}

func readAgentPolicy(home string) approval.Policy {
	if home == "" {
		return nil
	}
	path := filepath.Join(home, approvalsFile)
	buf, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			warnStderr("approvals: %s: unreadable, its rules do not apply: %v\n", path, err)
		}
		return nil
	}
	pol, errs := approval.ParsePolicy(string(buf))
	for _, e := range errs {
		warnStderr("approvals: %s: %v\n", path, e)
	}
	return pol
}

func sessionPolicy(hostState *state.State, agents *agent.Store, session string) (approval.Policy, approval.Mode) {
	var daemon approval.Policy
	for _, raw := range hostState.ApprovalRules() {
		rule, err := approval.ParseRule(raw)
		if err != nil {
			warnStderr("approvals: state rule %q dropped, it no longer applies: %v\n", raw, err)
			continue
		}
		daemon = append(daemon, rule)
	}
	var mode approval.Mode
	var home string
	sessions := hostState.SnapshotSessions()
	for idx := range sessions {
		entry := sessions[idx]
		if entry.Name == session {
			mode = approval.Mode(entry.Approvals)
			if agents != nil && entry.Agent != "" {
				if a, ok := agents.Get(entry.Agent); ok {
					home = a.Home
				}
			}
			break
		}
	}
	return approval.Merge(daemon, readAgentPolicy(home)), mode
}
