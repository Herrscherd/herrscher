package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func approveReg(t *testing.T) (*cli.Registry, *state.State) {
	t.Helper()
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	reg := &cli.Registry{}
	if err := addApproveCommands(reg, st, nil); err != nil {
		t.Fatalf("addApproveCommands: %v", err)
	}
	return reg, st
}

func TestApproveRuleAddsListsRemoves(t *testing.T) {
	reg, _ := approveReg(t)
	ctx := context.Background()
	if _, err := reg.Dispatch(ctx, []string{"approve", "rule", "ask Bash(git push*)"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	out, err := reg.Dispatch(ctx, []string{"approve", "rule"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "ask Bash(git push*)") {
		t.Fatalf("list %q must show the rule", out)
	}
	if _, err := reg.Dispatch(ctx, []string{"approve", "rule", "--rm", "ask Bash(git push*)"}); err != nil {
		t.Fatalf("rm: %v", err)
	}
	out, _ = reg.Dispatch(ctx, []string{"approve", "rule"})
	if strings.Contains(out, "git push") {
		t.Fatalf("list %q must no longer show the rule", out)
	}
}

func TestApproveRuleRefusesNonsense(t *testing.T) {
	reg, st := approveReg(t)
	if _, err := reg.Dispatch(context.Background(), []string{"approve", "rule", "maybe Bash(ls)"}); err == nil {
		t.Fatal("an unparseable rule must be refused")
	}
	if len(st.ApprovalRules()) != 0 {
		t.Fatal("a refused rule must not have been stored")
	}
}

func TestApproveModeRefusesUnknownMode(t *testing.T) {
	reg, st := approveReg(t)
	if err := st.AddSession(state.Session{Name: "s1", ChannelID: "c1"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if _, err := reg.Dispatch(context.Background(), []string{"approve", "mode", "s1", "loose"}); err == nil {
		t.Fatal("an unknown mode must be refused, and name the three that exist")
	}
	if _, err := reg.Dispatch(context.Background(), []string{"approve", "mode", "s1", "bypass"}); err != nil {
		t.Fatalf("bypass: %v", err)
	}
	// The mixed form: the flag names the session, the bare argument the mode.
	if _, err := reg.Dispatch(context.Background(), []string{"approve", "mode", "--session", "s1", "strict"}); err != nil {
		t.Fatalf("mixed form: %v", err)
	}
	// And a session named with nothing after it is still a usage error.
	if _, err := reg.Dispatch(context.Background(), []string{"approve", "mode", "s1"}); err == nil {
		t.Fatal("a mode with no stance to set must be refused")
	}
}

// Empty is `[]` on both sides. A caller that has to tell `null` from `[]` for
// one verb and not the other is being asked to remember which is which.
func TestApproveEmptyJSONIsAnEmptyList(t *testing.T) {
	reg, _ := approveReg(t)
	for _, argv := range [][]string{{"approve", "rule", "--json"}, {"approve", "list", "--json"}} {
		out, err := reg.Dispatch(context.Background(), argv)
		if err != nil {
			t.Fatalf("%v: %v", argv, err)
		}
		if out != "[]" {
			t.Fatalf("%v answered %q, want []", argv, out)
		}
	}
}

func TestApproveListEmpty(t *testing.T) {
	reg, _ := approveReg(t)
	out, err := reg.Dispatch(context.Background(), []string{"approve", "list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if out == "" {
		t.Fatal("an empty list must say so rather than answer with nothing")
	}
}

// The whole of sessionPolicy: the session lookup, the mode it carries, the
// agent home it resolves, and the daemon rules it parses on the way.
func TestSessionPolicyMergesAgentFile(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "a1")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, approvalsFile), []byte("deny Bash(git push*)\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	// "nonsense" is what a hand-edited state.json looks like: the verbs parse
	// before they store, so it can only get in that way. It must be dropped
	// without taking the rules beside it down.
	for _, r := range []string{"ask Bash(git push*)", "ask Write", "nonsense"} {
		if err := st.AddApprovalRule(r); err != nil {
			t.Fatalf("AddApprovalRule %q: %v", r, err)
		}
	}
	if err := st.AddSession(state.Session{Name: "s1", ChannelID: "c1", Agent: "a1", Approvals: "strict"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	pol, mode := sessionPolicy(st, agent.NewStore(root), "s1")
	if mode != approval.ModeStrict {
		t.Fatalf("mode = %q, want the session's own stance", mode)
	}
	if d, _ := pol.Decide(approval.Request{Tool: "Bash", Subject: "git push"}); d != approval.Deny {
		t.Fatalf("got %q, want deny: the agent's file tightens the daemon's ask", d)
	}
	if d, _ := pol.Decide(approval.Request{Tool: "Write"}); d != approval.Ask {
		t.Fatalf("got %q, want ask: a daemon rule the agent says nothing about still applies", d)
	}
}

func TestSessionPolicyOfAnUnknownSession(t *testing.T) {
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if err := st.AddApprovalRule("deny Bash(rm -rf*)"); err != nil {
		t.Fatalf("AddApprovalRule: %v", err)
	}
	pol, mode := sessionPolicy(st, nil, "never-created")
	if mode != "" {
		t.Fatalf("mode = %q, want the unset mode of a session that is not there", mode)
	}
	if d, _ := pol.Decide(approval.Request{Tool: "Bash", Subject: "rm -rf /"}); d != approval.Deny {
		t.Fatalf("got %q, want deny: the daemon's rules apply with or without a session row", d)
	}
}

func TestApproveAllowSettlesAWaitingRequest(t *testing.T) {
	reg, _ := approveReg(t)
	pol := approval.Policy{{Decision: approval.Ask, Tool: "Bash", Pattern: "git push*"}}
	done := make(chan approval.Decision, 1)
	go func() {
		d, _ := askApproval(context.Background(), "s1", approval.Request{Tool: "Bash", Subject: "git push"}, pol, approval.ModeAsk, 5*time.Second)
		done <- d
	}()

	var id string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p := pendingApprovals(); len(p) == 1 {
			id = p[0].ID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("the request was never registered")
	}
	out, err := reg.Dispatch(context.Background(), []string{"approve", "allow", id})
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if !strings.Contains(out, id) {
		t.Fatalf("out = %q, want the id it settled", out)
	}
	select {
	case d := <-done:
		if d != approval.Allow {
			t.Fatalf("got %q, want allow: the operator said so", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the waiter never got the answer")
	}
}

func TestApproveAllowUnknownIDReportsIt(t *testing.T) {
	reg, _ := approveReg(t)
	out, err := reg.Dispatch(context.Background(), []string{"approve", "allow", "nope"})
	if err == nil {
		t.Fatalf("out = %q, want an error: claiming success for a request nobody is waiting on reads as an allowed call", out)
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want the id named", err)
	}
}

func TestReadAgentPolicyMissingFile(t *testing.T) {
	if p := readAgentPolicy(t.TempDir()); len(p) != 0 {
		t.Fatal("an agent with no APPROVALS file adds no rule")
	}
}
