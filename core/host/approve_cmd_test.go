package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
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

func TestSessionPolicyMergesAgentFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "APPROVALS"), []byte("deny Bash(git push*)\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	pol := approval.Merge(
		approval.Policy{{Decision: approval.Ask, Tool: "Bash", Pattern: "git push*"}},
		readAgentPolicy(dir),
	)
	if d, _ := pol.Decide(approval.Request{Tool: "Bash", Subject: "git push"}); d != approval.Deny {
		t.Fatalf("got %q, want deny: the agent's file tightens", d)
	}
}

func TestReadAgentPolicyMissingFile(t *testing.T) {
	if p := readAgentPolicy(t.TempDir()); len(p) != 0 {
		t.Fatal("an agent with no APPROVALS file adds no rule")
	}
}
