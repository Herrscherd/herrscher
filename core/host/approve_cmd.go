package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// approvalsFile is the file an agent tightens the daemon's rules with, beside
// the TAGS, backend, cmd and host files its home already carries.
const approvalsFile = "APPROVALS"

// addApproveCommands registers the approve verbs: five for an operator, two for
// a machine. Like host and schedule, they are neutral argv, so a chat gateway
// binds them as they are.
func addApproveCommands(reg *cli.Registry, st *state.State, agents *agent.Store) error {
	if err := reg.Add(contracts.New("approve", "list").
		Help("list the actions waiting for an answer").
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			pending := pendingApprovals()
			// Age is truncated to the second, so two requests raised in the same
			// second compare equal. Stable, with the id as a tiebreak, so the same
			// waiting set lists in the same order every call.
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
					if rules == nil {
						rules = []string{} // an empty rule set is `[]`, like `approve list`, never `null`
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
			wait := st.ApprovalWait()
			warnWaitOutlivesTheHook(os.Stderr, wait)
			d, reason := askApproval(ctx, session, req, pol, mode, wait)
			b, err := json.Marshal(struct {
				Decision string `json:"decision"`
				Reason   string `json:"reason,omitempty"`
			}{Decision: string(d), Reason: reason})
			return string(b), err
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New("approve", "hook").
		Help("machine verb: read a PreToolUse payload on stdin and answer it").
		Do(func(ctx context.Context, _ contracts.Input) (string, error) {
			// Runs in the short-lived process the vendor spawned, never in the
			// daemon: it is the caller that must reach the command socket, and
			// the payload it answers is on that process's own stdin.
			//
			// This registry is also the daemon's own, so the verb shows up in the
			// command palette. Selected there, the read below would take the
			// terminal Bubbletea holds in raw mode: it would never see EOF, would
			// steal the operator's keystrokes, and would block that command for
			// good. Refusing on a terminal is the guard. A real hook is spawned
			// with a pipe on stdin and never reaches it, and the binary answers
			// `approve hook` before this registry exists anyway.
			if term.IsTerminal(int(os.Stdin.Fd())) {
				return "", errors.New("approve hook is a machine verb: it reads a PreToolUse payload on stdin")
			}
			var out strings.Builder
			RunApprovalHook(ctx, os.Stdin, &out, os.Stderr)
			return strings.TrimRight(out.String(), "\n"), nil
		})); err != nil {
		return err
	}

	return reg.Add(contracts.New("approve", "mode").
		Help("set a session's stance: ask (default), bypass, or strict").
		ValueParam("session", "session name; also takes a bare argument", false).
		ValueParam("mode", "ask | bypass | strict; also takes a second bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			// The name takes the first bare argument when no flag gave it, and the
			// mode the last one still unclaimed. Counting from the end rather than
			// from a fixed index is what makes the mixed form work: `approve mode
			// --session s1 bypass` leaves one positional, `approve mode s1 bypass`
			// leaves two.
			rest := in.Rest
			name := strings.TrimSpace(in.Get("session"))
			if name == "" && len(rest) > 0 {
				name, rest = strings.TrimSpace(rest[0]), rest[1:]
			}
			mode := strings.TrimSpace(in.Get("mode"))
			if mode == "" && len(rest) > 0 {
				mode = strings.TrimSpace(rest[len(rest)-1])
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

// warnWaitOutlivesTheHook says so when the configured wait is longer than the
// vendor will wait for the hook to answer. Past that point the CLI stops
// waiting and runs the tool call, while the request is still listed as waiting
// and the operator still believes a human is being asked: a silent allow, which
// is the one outcome this feature exists to prevent.
//
// It warns and does not cap. The wait is the operator's to choose, and a wait
// quietly shortened to fit the vendor would be its own surprise. Written on
// every ask that would run long rather than once at startup, because the wait
// is read from state at each ask and can change under a running daemon.
func warnWaitOutlivesTheHook(w io.Writer, wait time.Duration) {
	if wait <= agent.HookWait {
		return
	}
	fmt.Fprintf(w, "approvals: a %s wait outlives the %s the CLI gives the hook: past %s the tool call runs and nobody is told\n", wait, agent.HookWait, agent.HookWait)
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
//
// Only an absent file means "no rules". Any other read error (a mode change, a
// home on a filesystem that went away) drops rules that were written to tighten
// something, so it is said on stderr before this falls open. Falling open is
// deliberate: this is a guardrail against mistakes, not a sandbox, and an
// unreadable file must not wedge every tool call. Silence is what it must not do.
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

// sessionPolicy is what applies to one session right now: the daemon's rules,
// tightened by the agent's own, plus the session's mode. Both are read at the
// moment of asking, so a rule added while a session runs binds that session.
func sessionPolicy(st *state.State, agents *agent.Store, session string) (approval.Policy, approval.Mode) {
	var daemon approval.Policy
	for _, raw := range st.ApprovalRules() {
		r, err := approval.ParseRule(raw)
		if err != nil {
			// The verbs parse before they store, so this is a hand-edited
			// state.json. The rule is dropped, which allows every call it was
			// written to stop, and that is exactly why it is said out loud.
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
