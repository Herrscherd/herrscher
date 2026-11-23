package bridge

import (
	"math"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/timeline"
)

func relayedToolStart(id string) contracts.BackendEvent {
	return contracts.BackendEvent{Kind: backendKindTool, Tool: "read", Detail: id, ToolCall: &contracts.ToolCall{ID: id, Tool: "read", Detail: id, State: contracts.ToolCallStarted}}
}

func TestResultWithoutUsageKeepsTheRunningTotal(t *testing.T) {
	var usage turnUsage
	usage.absorb(contracts.BackendEvent{Kind: backendKindUsage, OutTokens: 150})
	usage.absorb(contracts.BackendEvent{Kind: backendKindResult, OutTokens: 0})
	if usage.outTok != 150 {
		t.Fatalf("total = %d after a result without usage, want 150", usage.outTok)
	}
}

func TestResultWithUsageReplacesTheRunningTotal(t *testing.T) {
	var usage turnUsage
	usage.absorb(contracts.BackendEvent{Kind: backendKindUsage, OutTokens: 150})
	usage.absorb(contracts.BackendEvent{Kind: backendKindResult, OutTokens: 120})
	if usage.outTok != 120 {
		t.Fatalf("total = %d after a result of 120, want 120", usage.outTok)
	}
}

func TestBridgeTotalAndCallTokensAgreeOnTheSequence(t *testing.T) {
	var sink recordingSink
	var usage turnUsage
	relay := func(event contracts.BackendEvent) {
		relayBackendEvent(&sink, &usage, event)
	}
	relay(relayedToolStart("a"))
	relay(contracts.BackendEvent{Kind: backendKindUsage, OutTokens: 150})
	relay(relayedToolStart("b"))
	relay(contracts.BackendEvent{Kind: backendKindResult, OutTokens: 0})
	relay(contracts.BackendEvent{Kind: backendKindUsage, OutTokens: 30})
	relay(relayedToolStart("c"))

	var rec timeline.Recorder
	for _, event := range sink.events {
		if _, _, err := rec.Apply(event); err != nil {
			t.Fatalf("Apply(%+v) error = %v", event, err)
		}
	}
	if usage.outTok != 180 {
		t.Fatalf("bridge total = %d, want 180: a result of 0 must not erase the 150 counted before it", usage.outTok)
	}
	sum := 0
	for _, call := range rec.Calls() {
		sum += call.Tokens
	}
	if sum != usage.outTok {
		t.Fatalf("calls sum to %d tokens, the bridge total is %d", sum, usage.outTok)
	}
}

func TestResultWithoutUsageKeepsTheInputSideAndTheCost(t *testing.T) {
	var usage turnUsage
	usage.absorb(contracts.BackendEvent{Kind: backendKindUsage, OutTokens: 150, InTokens: 900, CacheRead: 300, CacheCreate: 40})
	usage.absorb(contracts.BackendEvent{Kind: backendKindResult, Cost: 0.0042})
	if usage.inTok != 900 || usage.cacheRd != 300 || usage.cacheCr != 40 {
		t.Fatalf("input side = (%d, %d, %d) after a result without usage, want (900, 300, 40)", usage.inTok, usage.cacheRd, usage.cacheCr)
	}
	if usage.cost != 0.0042 {
		t.Fatalf("cost = %v after a result without usage, want 0.0042", usage.cost)
	}
}

func TestUsageWithZeroInputDoesNotEraseTheEarlierInput(t *testing.T) {
	var usage turnUsage
	usage.absorb(contracts.BackendEvent{Kind: backendKindUsage, OutTokens: 150, InTokens: 900})
	usage.absorb(contracts.BackendEvent{Kind: backendKindUsage, OutTokens: 20})
	if usage.inTok != 900 {
		t.Fatalf("input = %d after a usage with no input, want 900", usage.inTok)
	}
	if usage.outTok != 170 {
		t.Fatalf("output = %d, want 170: the two usages add up", usage.outTok)
	}
}

func TestResultWithoutCostKeepsTheEarlierCost(t *testing.T) {
	var usage turnUsage
	usage.absorb(contracts.BackendEvent{Kind: backendKindResult, Cost: 0.0042})
	usage.absorb(contracts.BackendEvent{Kind: backendKindResult, Cost: 0})
	if usage.cost != 0.0042 {
		t.Fatalf("cost = %v after a result reporting no cost, want the earlier 0.0042", usage.cost)
	}
}

func usageEvent(out, in, cacheRd, cacheCr int) contracts.BackendEvent {
	return contracts.BackendEvent{Kind: backendKindUsage, OutTokens: out, InTokens: in, CacheRead: cacheRd, CacheCreate: cacheCr}
}

func resultEvent(out, in, cacheRd, cacheCr int) contracts.BackendEvent {
	return contracts.BackendEvent{Kind: backendKindResult, OutTokens: out, InTokens: in, CacheRead: cacheRd, CacheCreate: cacheCr}
}

type figureCase struct {
	name  string
	value int
}

func notPositiveFigures() []figureCase {
	return []figureCase{{"negative", -40}, {"zero", 0}}
}

func TestUsageOutputThatIsNotPositiveIsNotAdded(t *testing.T) {
	for _, tc := range notPositiveFigures() {
		t.Run(tc.name, func(t *testing.T) {
			var usage turnUsage
			usage.absorb(usageEvent(150, 0, 0, 0))
			usage.absorb(usageEvent(tc.value, 0, 0, 0))
			if usage.outTok != 150 {
				t.Fatalf("total = %d after a usage of %d, want 150", usage.outTok, tc.value)
			}
		})
	}
}

func TestResultOutputThatIsNotPositiveKeepsTheTotal(t *testing.T) {
	for _, tc := range notPositiveFigures() {
		t.Run(tc.name, func(t *testing.T) {
			var usage turnUsage
			usage.absorb(usageEvent(150, 0, 0, 0))
			usage.absorb(resultEvent(tc.value, 0, 0, 0))
			if usage.outTok != 150 {
				t.Fatalf("total = %d after a result of %d, want 150", usage.outTok, tc.value)
			}
		})
	}
}

func TestUsageInputThatIsNotPositiveKeepsTheEarlierInput(t *testing.T) {
	for _, tc := range notPositiveFigures() {
		t.Run(tc.name, func(t *testing.T) {
			var usage turnUsage
			usage.absorb(usageEvent(0, 900, 0, 0))
			usage.absorb(usageEvent(0, tc.value, 0, 0))
			if usage.inTok != 900 {
				t.Fatalf("input = %d after a usage of %d, want 900", usage.inTok, tc.value)
			}
		})
	}
}

func TestResultInputThatIsNotPositiveKeepsTheEarlierInput(t *testing.T) {
	for _, tc := range notPositiveFigures() {
		t.Run(tc.name, func(t *testing.T) {
			var usage turnUsage
			usage.absorb(usageEvent(0, 900, 0, 0))
			usage.absorb(resultEvent(0, tc.value, 0, 0))
			if usage.inTok != 900 {
				t.Fatalf("input = %d after a result of %d, want 900", usage.inTok, tc.value)
			}
		})
	}
}

func TestUsageCacheReadThatIsNotPositiveKeepsTheEarlierCacheRead(t *testing.T) {
	for _, tc := range notPositiveFigures() {
		t.Run(tc.name, func(t *testing.T) {
			var usage turnUsage
			usage.absorb(usageEvent(0, 0, 300, 0))
			usage.absorb(usageEvent(0, 0, tc.value, 0))
			if usage.cacheRd != 300 {
				t.Fatalf("cache read = %d after a usage of %d, want 300", usage.cacheRd, tc.value)
			}
		})
	}
}

func TestResultCacheReadThatIsNotPositiveKeepsTheEarlierCacheRead(t *testing.T) {
	for _, tc := range notPositiveFigures() {
		t.Run(tc.name, func(t *testing.T) {
			var usage turnUsage
			usage.absorb(usageEvent(0, 0, 300, 0))
			usage.absorb(resultEvent(0, 0, tc.value, 0))
			if usage.cacheRd != 300 {
				t.Fatalf("cache read = %d after a result of %d, want 300", usage.cacheRd, tc.value)
			}
		})
	}
}

func TestUsageCacheCreateThatIsNotPositiveKeepsTheEarlierCacheCreate(t *testing.T) {
	for _, tc := range notPositiveFigures() {
		t.Run(tc.name, func(t *testing.T) {
			var usage turnUsage
			usage.absorb(usageEvent(0, 0, 0, 40))
			usage.absorb(usageEvent(0, 0, 0, tc.value))
			if usage.cacheCr != 40 {
				t.Fatalf("cache create = %d after a usage of %d, want 40", usage.cacheCr, tc.value)
			}
		})
	}
}

func TestResultCacheCreateThatIsNotPositiveKeepsTheEarlierCacheCreate(t *testing.T) {
	for _, tc := range notPositiveFigures() {
		t.Run(tc.name, func(t *testing.T) {
			var usage turnUsage
			usage.absorb(usageEvent(0, 0, 0, 40))
			usage.absorb(resultEvent(0, 0, 0, tc.value))
			if usage.cacheCr != 40 {
				t.Fatalf("cache create = %d after a result of %d, want 40", usage.cacheCr, tc.value)
			}
		})
	}
}

func TestResultCostThatIsNegativeKeepsTheEarlierCost(t *testing.T) {
	var usage turnUsage
	usage.absorb(contracts.BackendEvent{Kind: backendKindResult, Cost: 0.0042})
	usage.absorb(contracts.BackendEvent{Kind: backendKindResult, Cost: -0.5})
	if usage.cost != 0.0042 {
		t.Fatalf("cost = %v after a result reporting a negative cost, want the earlier 0.0042", usage.cost)
	}
}

func TestResultCostThatIsInfiniteKeepsTheEarlierCost(t *testing.T) {
	var usage turnUsage
	usage.absorb(contracts.BackendEvent{Kind: backendKindResult, Cost: 0.0042})
	usage.absorb(contracts.BackendEvent{Kind: backendKindResult, Cost: math.Inf(1)})
	if usage.cost != 0.0042 {
		t.Fatalf("cost = %v after a result reporting +Inf, want the earlier 0.0042", usage.cost)
	}
}
