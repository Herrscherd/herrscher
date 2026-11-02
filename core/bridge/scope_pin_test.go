package bridge

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type fixedResolver struct {
	answer string
	asked  int
}

func (r *fixedResolver) Resolve(context.Context, string) string {
	r.asked++
	return r.answer
}

type scopedOrch struct {
	contracts.Orchestrator
	got contracts.MemoryScope
}

func (o *scopedOrch) SetScope(s contracts.MemoryScope) { o.got = s }

func TestPinSettlesOnceAndTellsTheOrchestrator(t *testing.T) {
	r := &fixedResolver{answer: "neublox"}
	o := &scopedOrch{}
	p := &scopePin{resolve: r, current: "herrscher", agent: "tui", orch: o}

	if got := p.settle(context.Background(), "je bosse sur neublox"); got != "neublox" {
		t.Fatalf("settle = %q, want neublox", got)
	}
	if o.got.Project != contracts.ProjectKey("neublox") || o.got.Agent != contracts.AgentKey("tui") {
		t.Fatalf("scope = %+v, want both roots", o.got)
	}
	if got := p.settle(context.Background(), "et maintenant herrscher"); got != "" {
		t.Fatalf("a second turn re-opened the question: %q", got)
	}
	if r.asked != 1 {
		t.Fatalf("resolver asked %d times, want 1", r.asked)
	}
}

func TestPinKeepsTheLaunchCandidateWhenNothingIsNamed(t *testing.T) {
	o := &scopedOrch{}
	p := &scopePin{resolve: &fixedResolver{answer: ""}, current: "herrscher", agent: "tui", orch: o}
	if got := p.settle(context.Background(), "on continue"); got != "herrscher" {
		t.Fatalf("settle = %q, want the launch candidate back", got)
	}
	if o.got.Project != "" {
		t.Fatal("nothing changed, so nothing should have been re-rooted")
	}
}

func TestPinStaysSilentWithNothingToSay(t *testing.T) {
	p := &scopePin{resolve: &fixedResolver{}, orch: &scopedOrch{}}
	if got := p.settle(context.Background(), "salut"); got != "" {
		t.Fatalf("settle = %q, want empty", got)
	}
}

func TestPinToleratesAnOrchestratorThatCannotBeRerooted(t *testing.T) {
	p := &scopePin{resolve: &fixedResolver{answer: "neublox"}, current: "herrscher", orch: nil}
	if got := p.settle(context.Background(), "neublox"); got != "neublox" {
		t.Fatalf("settle = %q, want neublox", got)
	}
}

func TestNoPinSettlesNothing(t *testing.T) {
	var p *scopePin
	if got := p.settle(context.Background(), "neublox"); got != "" {
		t.Fatalf("settle = %q, want empty — a pinned session is never re-scoped", got)
	}
}
