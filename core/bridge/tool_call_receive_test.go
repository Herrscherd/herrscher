package bridge

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

type recordingSink struct {
	events []contracts.Event
}

func (s *recordingSink) Emit(event contracts.Event) {
	s.events = append(s.events, event)
}

func startedCall() *contracts.ToolCall {
	return &contracts.ToolCall{ID: "a", Tool: "read", Detail: "env.go", State: contracts.ToolCallStarted}
}

func TestRefusedToolCallKeepsTheRestOfTheEvent(t *testing.T) {
	event := contracts.BackendEvent{Kind: backendKindText, Detail: "hello", InTokens: 7, OutTokens: 3, ToolCall: startedCall()}
	got := receiveOne(event)
	if got.ToolCall != nil {
		t.Fatalf("ToolCall = %+v, want nil: a text event cannot carry a call", got.ToolCall)
	}
	if got.Detail != "hello" || got.InTokens != 7 || got.OutTokens != 3 {
		t.Fatalf("event = %+v, want its text and tokens kept", got)
	}
}

func TestReceivedToolCallIsStampedInUTC(t *testing.T) {
	event := contracts.BackendEvent{Kind: backendKindTool, Tool: "read", Detail: "env.go", ToolCall: startedCall()}
	before := time.Now()
	got := receiveOne(event)
	after := time.Now()
	if got.ToolCall == nil {
		t.Fatal("ToolCall = nil, want the valid call kept")
	}
	at := got.ToolCall.At
	if at.Before(before.Truncate(time.Second)) || at.After(after) {
		t.Fatalf("At = %v, want between %v and %v", at, before, after)
	}
	if at.Location() != time.UTC {
		t.Fatalf("At location = %v, want UTC", at.Location())
	}
	if !event.ToolCall.At.IsZero() {
		t.Fatal("the backend's call was mutated: it must stay zero")
	}
}

func TestStampedBackendCallIsRefused(t *testing.T) {
	call := startedCall()
	call.At = time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	got := receiveOne(contracts.BackendEvent{Kind: backendKindTool, Tool: "read", Detail: "env.go", ToolCall: call})
	if got.ToolCall != nil {
		t.Fatalf("ToolCall = %+v, want nil: the backend must leave At zero", got.ToolCall)
	}
}

func TestToolCallWithAnOverlongIDOrToolIsRefused(t *testing.T) {
	long := strings.Repeat("x", maxToolCallNameBytes+1)
	for name, mutate := range map[string]func(*contracts.ToolCall){
		"id":   func(call *contracts.ToolCall) { call.ID = long },
		"tool": func(call *contracts.ToolCall) { call.Tool = long },
	} {
		t.Run(name, func(t *testing.T) {
			call := startedCall()
			mutate(call)
			got := receiveOne(contracts.BackendEvent{Kind: backendKindTool, Tool: call.Tool, Detail: call.Detail, ToolCall: call})
			if got.ToolCall != nil {
				t.Fatalf("ToolCall with an overlong %s was kept, want it refused", name)
			}
			if got.Detail != call.Detail {
				t.Fatalf("event = %+v, want its text kept", got)
			}
		})
	}
}

func TestToolCallAtTheNameLimitIsKept(t *testing.T) {
	call := startedCall()
	call.ID = strings.Repeat("x", maxToolCallNameBytes)
	got := receiveOne(contracts.BackendEvent{Kind: backendKindTool, Tool: "read", Detail: "env.go", ToolCall: call})
	if got.ToolCall == nil {
		t.Fatal("ToolCall = nil, want a call at the limit kept")
	}
}

func TestToolDoneCarriesCallWithoutStatusLine(t *testing.T) {
	sink := &recordingSink{}
	done := contracts.BackendEvent{Kind: backendKindToolDone, ToolCall: &contracts.ToolCall{ID: "a", Tool: "read", State: contracts.ToolCallSucceeded}}
	emitBackendEvent(sink, receiveOne(done), 12, 0, 0, 0)
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want one tool_done and no status", len(sink.events))
	}
	event := sink.events[0]
	if event.T != eventwire.EventToolDone || event.ToolCall == nil || event.ToolCall.ID != "a" {
		t.Fatalf("event = %+v, want tool_done carrying call a", event)
	}
	if event.Text != "" || event.Tokens != 12 {
		t.Fatalf("event = %+v, want no text and the tokens kept", event)
	}
}

func TestToolDoneWithoutCallEmitsNothing(t *testing.T) {
	sink := &recordingSink{}
	emitBackendEvent(sink, receiveOne(contracts.BackendEvent{Kind: backendKindToolDone}), 0, 0, 0, 0)
	if len(sink.events) != 0 {
		t.Fatalf("events = %+v, want none", sink.events)
	}
}

func TestToolStartKeepsToolAndDetailAndCall(t *testing.T) {
	sink := &recordingSink{}
	start := receiveOne(contracts.BackendEvent{Kind: backendKindTool, Tool: "read", Detail: "env.go", ToolCall: startedCall()})
	emitBackendEvent(sink, start, 0, 0, 0, 0)
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want one status", len(sink.events))
	}
	event := sink.events[0]
	if event.T != eventwire.EventStatus || event.Text != "read env.go" || event.ToolCall == nil {
		t.Fatalf("event = %+v, want a status reading \"read env.go\" with its call", event)
	}
}

func receiveOne(event contracts.BackendEvent) contracts.BackendEvent {
	var guard toolCallGuard
	return guard.receive(event)
}
