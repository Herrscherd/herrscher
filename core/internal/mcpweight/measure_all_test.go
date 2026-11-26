package mcpweight

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func TestMeasureReportsCanceledBeforeStartAsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := Measure(ctx, fakeServer("two-pages"), DefaultTimeout)
	if result.Measured || result.Cause != CauseCanceled {
		t.Fatalf("Measure = %+v, want a canceled measure, not a start failure", result)
	}
}

func TestMeasureReportsMissingCommandWithoutClosingTwice(t *testing.T) {
	result := Measure(context.Background(), agent.MCPServer{Name: "gone", Command: missingCommand}, DefaultTimeout)
	if result.Cause != CauseStart {
		t.Fatalf("Measure = %+v, want a start failure", result)
	}
	if strings.Contains(result.Detail, "already closed") {
		t.Fatalf("Detail = %q, the failed start must not close its descriptors a second time", result.Detail)
	}
	if !strings.Contains(result.Detail, "no such file") {
		t.Fatalf("Detail = %q, want the reason the command did not start", result.Detail)
	}
}

func TestMeasureAllReportsBlockedServersAsBudgetExceededWithinTheBudget(t *testing.T) {
	servers := make([]agent.MCPServer, 0, 6)
	for range 6 {
		servers = append(servers, fakeServer("silent"))
	}
	const budget = time.Second
	started := time.Now()
	results := NewMeasurer().MeasureAll(context.Background(), servers, budget)
	elapsed := time.Since(started)
	if len(results) != len(servers) {
		t.Fatalf("MeasureAll returned %d results for %d servers", len(results), len(servers))
	}
	for _, result := range results {
		if result.Measured || result.Cause != CauseBudget || result.Detail == "" {
			t.Fatalf("result = %+v, want a server cut short by the budget with its reason", result)
		}
	}
	if elapsed > budget+10*time.Second {
		t.Fatalf("MeasureAll took %s, the budget of %s did not bound it", elapsed, budget)
	}
}

func TestMeasureAllReportsInTheOrderGiven(t *testing.T) {
	servers := []agent.MCPServer{fakeServer("two-pages"), fakeServer("rejecting"), fakeServer("exit"), fakeServer("garbage")}
	results := NewMeasurer().MeasureAll(context.Background(), servers, DefaultBudget)
	wantCauses := []Cause{"", CauseRejected, CauseExited, CauseProtocol}
	for index, result := range results {
		if result.Name != servers[index].Name {
			t.Fatalf("result %d is %q, want %q: the report must follow the order given", index, result.Name, servers[index].Name)
		}
		if result.Cause != wantCauses[index] {
			t.Errorf("result %d cause = %q, want %q", index, result.Cause, wantCauses[index])
		}
	}
	if !results[0].Measured || results[0].Tools != fakeToolCount {
		t.Fatalf("first result = %+v, want the tools", results[0])
	}
}

func TestMeasureAllReportsEveryServerCanceledWhenTheParentIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	servers := []agent.MCPServer{fakeServer("two-pages"), fakeServer("silent"), fakeServer("two-pages")}
	for index, result := range NewMeasurer().MeasureAll(ctx, servers, DefaultBudget) {
		if result.Measured || result.Cause != CauseCanceled {
			t.Fatalf("result %d = %+v, want canceled, not a start failure", index, result)
		}
	}
}

func TestMeasureAllOfNoServersIsEmpty(t *testing.T) {
	if results := NewMeasurer().MeasureAll(context.Background(), nil, DefaultBudget); len(results) != 0 {
		t.Fatalf("MeasureAll = %v, want nothing", results)
	}
}
