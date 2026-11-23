package bridge

import (
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
