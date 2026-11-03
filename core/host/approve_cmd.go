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

func addApproveCommands(reg *cli.Registry, st *state.State, agents *agent.Store) error {
	if err := reg.Add(contracts.New(nsApprove, "list").
		Help("list the actions waiting for an answer").
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			pending := pendingApprovals()
			sort.SliceStable(pending, func(i, j int) bool {
				if pending[i].Age != pending[j].Age {
					return pending[i].Age > pending[j].Age
				}
				return pending[i].ID < pending[j].ID
			})
			if in.JSON {
				b, err := json.Marshal(pending)
				return string(b), err
			}
			if len(pending) == 0 {
				return "nothing is waiting", nil
			}
			var b strings.Builder
			for _, p := range pending {
				fmt.Fprintf(&b, "- %s  %s  %s(%s)  waiting %s\n", p.ID, p.Session, p.Tool, p.Subject, p.Age)
			}
			return strings.TrimRight(b.String(), "\n"), nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New(nsApprove, "allow").
		Help("let a waiting action run").
		ValueParam("id", "request id; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			id := firstOf(in.Get("id"), in.Rest)
			if id == "" {
				return "", errRequestNameMissing
			}
			if !answerApproval(id, approval.Allow, "") {
				return "", &requestNotWaitingError{ID: id}
			}
			return "allowed " + id, nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New(nsApprove, "deny").
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
			if !answerApproval(id, approval.Deny, reason) {
				return "", &requestNotWaitingError{ID: id}
			}
			return "denied " + id, nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New(nsApprove, "rule").
		Help("add a rule, remove one with --rm, or list them with no argument").
		ValueParam("rule", "e.g. `ask Bash(git push*)`; also takes a bare argument", false).
		ValueParam("rm", "the rule to remove, written exactly as it is listed", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			if rm := strings.TrimSpace(in.Get("rm")); rm != "" {
				parsed, err := approval.ParseRule(rm)
				if err != nil {
					return "", err
				}
				removed, err := st.RemoveApprovalRule(parsed.String())
				if err != nil {
					return "", err
				}
				if !removed {
					return "", &noSuchRuleError{Rule: parsed.String()}
				}
				return "removed " + parsed.String(), nil
			}
			raw := firstOf(in.Get("rule"), in.Rest)
			if raw == "" {
				rules := st.ApprovalRules()
				if in.JSON {
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
			parsed, err := approval.ParseRule(raw)
			if err != nil {
				return "", err
			}
			if err := st.AddApprovalRule(parsed.String()); err != nil {
				return "", err
			}
			return "added " + parsed.String(), nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New(nsApprove, "ask").
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
			pol, mode := sessionPolicy(st, agents, session)
			d, reason := askApproval(ctx, session, req, pol, mode, st.ApprovalWait())
			b, err := json.Marshal(struct {
				Decision string `json:"decision"`
				Reason   string `json:"reason,omitempty"`
			}{Decision: string(d), Reason: reason})
			return string(b), err
		})); err != nil {
		return err
	}

	return reg.Add(contracts.New(nsApprove, "mode").
		Help("set a session's stance: ask (default), bypass, or strict").
		ValueParam(paramSession, "session name; also takes a bare argument", false).
		ValueParam("mode", "ask | bypass | strict; also takes a second bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			rest := in.Rest
			name := strings.TrimSpace(in.Get(paramSession))
			if name == "" && len(rest) > 0 {
				name, rest = strings.TrimSpace(rest[0]), rest[1:]
			}
			mode := strings.TrimSpace(in.Get("mode"))
			if mode == "" && len(rest) > 0 {
				mode = strings.TrimSpace(rest[len(rest)-1])
			}
			if name == "" || mode == "" {
				return "", errApproveModeUsage
			}
			switch approval.Mode(mode) {
			case approval.ModeAsk, approval.ModeBypass, approval.ModeStrict:
			default:
				return "", &unknownModeError{Mode: mode}
			}
			if err := st.SetSessionApprovals(name, mode); err != nil {
				return "", err
			}
			if mode != string(approval.ModeBypass) {
				return fmt.Sprintf("%s is now %s (a session created with --approvals bypass has no hook: recreate it for this to bite)", name, mode), nil
			}
			return fmt.Sprintf("%s is now %s", name, mode), nil
		}))
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
