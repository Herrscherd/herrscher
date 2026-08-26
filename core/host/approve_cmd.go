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

// approvalsFile is the file an agent tightens the daemon's rules with, beside
// the TAGS, backend, cmd and host files its home already carries.
const approvalsFile = "APPROVALS"

// addApproveCommands registers the approve verbs: five for an operator, one for
// a machine. Like host and schedule, they are neutral argv, so a chat gateway
// binds them as they are.
func addApproveCommands(reg *cli.Registry, st *state.State, agents *agent.Store) error {
	if err := reg.Add(contracts.New("approve", "list").
		Help("list the actions waiting for an answer").
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			pending := pendingApprovals()
			sort.Slice(pending, func(i, j int) bool { return pending[i].Age > pending[j].Age })
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

	if err := reg.Add(contracts.New("approve", "allow").
		Help("let a waiting action run").
		ValueParam("id", "request id; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			id := firstOf(in.Get("id"), in.Rest)
			if id == "" {
				return "", fmt.Errorf("name a request (see `approve list`)")
			}
			if !answerApproval(id, approval.Allow, "") {
				return "", fmt.Errorf("no request %q is waiting: it may have been answered, or timed out", id)
			}
			return "allowed " + id, nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New("approve", "deny").
		Help("refuse a waiting action; the reason is what the agent reads").
		ValueParam("id", "request id; also takes a bare argument", false).
		ValueParam("reason", "why, in the agent's own transcript", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			id := firstOf(in.Get("id"), in.Rest)
			if id == "" {
				return "", fmt.Errorf("name a request (see `approve list`)")
			}
			reason := strings.TrimSpace(in.Get("reason"))
			if reason != "" {
				reason = "herrscher: the operator refused: " + reason
			}
			if !answerApproval(id, approval.Deny, reason) {
				return "", fmt.Errorf("no request %q is waiting: it may have been answered, or timed out", id)
			}
			return "denied " + id, nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New("approve", "rule").
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
					return "", fmt.Errorf("no such rule: %s", parsed.String())
				}
				return "removed " + parsed.String(), nil
			}
			raw := firstOf(in.Get("rule"), in.Rest)
			if raw == "" {
				rules := st.ApprovalRules()
				if in.JSON {
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

	if err := reg.Add(contracts.New("approve", "ask").
		Help("machine verb: ask the daemon whether one tool call may run").
		ValueParam("session", "the session the call belongs to", true).
		ValueParam("tool", "tool name, e.g. Bash", true).
		ValueParam("subject", "the part a rule matches: the command, the path, the url", false).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			session := strings.TrimSpace(in.Get("session"))
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

	return reg.Add(contracts.New("approve", "mode").
		Help("set a session's stance: ask (default), bypass, or strict").
		ValueParam("session", "session name; also takes a bare argument", false).
		ValueParam("mode", "ask | bypass | strict; also takes a second bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			name := firstOf(in.Get("session"), in.Rest)
			mode := in.Get("mode")
			if mode == "" && len(in.Rest) > 1 {
				mode = strings.TrimSpace(in.Rest[1])
			}
			if name == "" || mode == "" {
				return "", fmt.Errorf("usage: approve mode <session> <ask|bypass|strict>")
			}
			switch approval.Mode(mode) {
			case approval.ModeAsk, approval.ModeBypass, approval.ModeStrict:
			default:
				return "", fmt.Errorf("unknown mode %q: ask, bypass or strict", mode)
			}
			if err := st.SetSessionApprovals(name, mode); err != nil {
				return "", err
			}
			// Said plainly rather than left to be discovered: a session created in
			// bypass carries no hook at all, so there is nothing for a later mode to
			// switch on.
			if mode != string(approval.ModeBypass) {
				return fmt.Sprintf("%s is now %s (a session created with --approvals bypass has no hook: recreate it for this to bite)", name, mode), nil
			}
			return fmt.Sprintf("%s is now %s", name, mode), nil
		}))
}

// firstOf takes a flag value, else the first bare argument. A gateway sends the
// flag; an operator types the name.
func firstOf(flag string, rest []string) string {
	if v := strings.TrimSpace(flag); v != "" {
		return v
	}
	if len(rest) > 0 {
		return strings.TrimSpace(rest[0])
	}
	return ""
}

// readAgentPolicy reads an agent's own rules. A missing file is no rules, and
// an unreadable line is skipped with a note on stderr: one bad line must not
// drop the rules around it, and must not silently widen anything either.
func readAgentPolicy(home string) approval.Policy {
	if home == "" {
		return nil
	}
	buf, err := os.ReadFile(filepath.Join(home, approvalsFile))
	if err != nil {
		return nil
	}
	pol, errs := approval.ParsePolicy(string(buf))
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "approvals: %s/%s: %v\n", home, approvalsFile, e)
	}
	return pol
}

// sessionPolicy is what applies to one session right now: the daemon's rules,
// tightened by the agent's own, plus the session's mode. Both are read at the
// moment of asking, so a rule added while a session runs binds that session.
func sessionPolicy(st *state.State, agents *agent.Store, session string) (approval.Policy, approval.Mode) {
	var daemon approval.Policy
	for _, raw := range st.ApprovalRules() {
		if r, err := approval.ParseRule(raw); err == nil {
			daemon = append(daemon, r)
		}
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
