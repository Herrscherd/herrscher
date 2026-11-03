package host

import (
	"context"
	"encoding/json"
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

func addApproveCommands(reg *cli.Registry, st *state.State, agents *agent.Store, approvals *approvalTable, sessions *sessionTable) error {
	return addVerbs(reg,
		approveListVerb(approvals),
		approveAllowVerb(approvals),
		approveDenyVerb(approvals),
		approveRuleVerb(st),
		approveAskVerb(st, agents, approvals, sessions),
		approveModeVerb(st),
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
	sort.SliceStable(pending, func(i, job int) bool {
		if pending[i].Age != pending[job].Age {
			return pending[i].Age > pending[job].Age
		}
		return pending[i].ID < pending[job].ID
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

func approveAllowVerb(approvals *approvalTable) contracts.Cmd {
	return contracts.New(nsApprove, "allow").
		Help("let a waiting action run").
		ValueParam("id", "request id; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			id := firstOf(in.Get("id"), in.Rest)
			if id == "" {
				return "", errRequestNameMissing
			}
			if !answerApproval(approvals, id, approval.Allow, "") {
				return "", &requestNotWaitingError{ID: id}
			}
			return "allowed " + id, nil
		})
}

func approveDenyVerb(approvals *approvalTable) contracts.Cmd {
	return contracts.New(nsApprove, "deny").
		Help("refuse a waiting action; the reason is what the agent reads").
		ValueParam("id", "request id; also takes a bare argument", false).
		ValueParam("reason", "why, in the agent's own transcript", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			id := firstOf(in.Get("id"), in.Rest)
			if id == "" {
				return "", errRequestNameMissing
			}
			reason := strings.TrimSpace(in.Get("reason"))
			if reason != "" {
				reason = "herrscher: the operator refused: " + reason
			}
			if !answerApproval(approvals, id, approval.Deny, reason) {
				return "", &requestNotWaitingError{ID: id}
			}
			return "denied " + id, nil
		})
}

func approveRuleVerb(store *state.State) contracts.Cmd {
	return contracts.New(nsApprove, "rule").
		Help("add a rule, remove one with --rm, or list them with no argument").
		ValueParam("rule", "e.g. `ask Bash(git push*)`; also takes a bare argument", false).
		ValueParam("rm", "the rule to remove, written exactly as it is listed", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			if rm := strings.TrimSpace(in.Get("rm")); rm != "" {
				return removeApprovalRule(store, rm)
			}
			raw := firstOf(in.Get("rule"), in.Rest)
			if raw == "" {
				return listApprovalRules(store, in.JSON)
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
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			session := strings.TrimSpace(in.Get(paramSession))
			req := approval.Request{
				Tool:    strings.TrimSpace(in.Get("tool")),
				Subject: in.Get("subject"),
			}
			pol, mode := sessionPolicy(store, agents, session)
			d, reason := askApproval(ctx, approvals, sessions, session, req, pol, mode, store.ApprovalWait())
			b, err := json.Marshal(struct {
				Decision string `json:"decision"`
				Reason   string `json:"reason,omitempty"`
			}{Decision: string(d), Reason: reason})
			return string(b), err
		})
}

func approveModeVerb(store *state.State) contracts.Cmd {
	return contracts.New(nsApprove, "mode").
		Help("set a session's stance: ask (default), bypass, or strict").
		ValueParam(paramSession, "session name; also takes a bare argument", false).
		ValueParam("mode", "ask | bypass | strict; also takes a second bare argument", false).
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
	case approval.ModeAsk, approval.ModeBypass, approval.ModeStrict:
	default:
		return "", &unknownModeError{Mode: mode}
	}
	if err := store.SetSessionApprovals(name, mode); err != nil {
		return "", err
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

func parseModeOperands(in contracts.Input) modeOperands {
	rest := in.Rest
	name := strings.TrimSpace(in.Get(paramSession))
	if name == "" && len(rest) > 0 {
		name, rest = strings.TrimSpace(rest[0]), rest[1:]
	}
	mode := strings.TrimSpace(in.Get("mode"))
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
			fmt.Fprintf(os.Stderr, "approvals: %s: unreadable, its rules do not apply: %v\n", path, err)
		}
		return nil
	}
	pol, errs := approval.ParsePolicy(string(buf))
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "approvals: %s: %v\n", path, e)
	}
	return pol
}

func sessionPolicy(st *state.State, agents *agent.Store, session string) (approval.Policy, approval.Mode) {
	var daemon approval.Policy
	for _, raw := range st.ApprovalRules() {
		r, err := approval.ParseRule(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "approvals: state rule %q dropped, it no longer applies: %v\n", raw, err)
			continue
		}
		daemon = append(daemon, r)
	}
	var mode approval.Mode
	var home string
	for _, s := range st.SnapshotSessions() {
		if s.Name == session {
			mode = approval.Mode(s.Approvals)
			if agents != nil && s.Agent != "" {
				if a, ok := agents.Get(s.Agent); ok {
					home = a.Home
				}
			}
			break
		}
	}
	return approval.Merge(daemon, readAgentPolicy(home)), mode
}
