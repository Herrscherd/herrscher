package otelexport

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/timeline"
)

func at(offset time.Duration) time.Time {
	return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC).Add(offset)
}

func recordingExporter(t *testing.T) (Exporter, *tracetest.InMemoryExporter) {
	t.Helper()
	memory := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(memory))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return newExporter(provider), memory
}

func spanNamed(t *testing.T, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	for index := range spans {
		if spans[index].Name == name {
			return spans[index]
		}
	}
	t.Fatalf("no span named %q among %d spans", name, len(spans))
	return tracetest.SpanStub{}
}

func attrValue(span tracetest.SpanStub, key attribute.Key) (attribute.Value, bool) {
	for _, kv := range span.Attributes {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func TestExportTurnWithTwoCallsCarriesGenAIAttributes(t *testing.T) {
	exporter, memory := recordingExporter(t)
	exporter.Export(context.Background(), Turn{
		Session: "alpha", Provider: "anthropic", Model: "claude-x", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted,
		InputTokens: 120, OutputTokens: 45, CacheReadTokens: 900, CacheCreationTokens: 30,
		Calls: []timeline.Call{
			{ID: "c1", Tool: "read", State: contracts.ToolCallSucceeded, StartedAt: at(0).Add(time.Second), EndedAt: ptrTime(at(0).Add(2 * time.Second))},
			{ID: "c2", Tool: "write", State: contracts.ToolCallSucceeded, StartedAt: at(0).Add(3 * time.Second), EndedAt: ptrTime(at(0).Add(4 * time.Second))},
		},
	})
	spans := memory.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 2 tool calls and 1 turn", len(spans))
	}
	parent := spanNamed(t, spans, "invoke_agent alpha")
	for key, want := range map[attribute.Key]string{
		semconv.GenAIOperationNameKey: "invoke_agent",
		semconv.GenAIProviderNameKey:  "anthropic",
		semconv.GenAIAgentNameKey:     "alpha",
		semconv.GenAIRequestModelKey:  "claude-x",
	} {
		if got, ok := attrValue(parent, key); !ok || got.AsString() != want {
			t.Errorf("%s = %q (present %v), want %q", key, got.AsString(), ok, want)
		}
	}
	for key, want := range map[attribute.Key]int64{
		semconv.GenAIUsageInputTokensKey:              120,
		semconv.GenAIUsageOutputTokensKey:             45,
		semconv.GenAIUsageCacheReadInputTokensKey:     900,
		semconv.GenAIUsageCacheCreationInputTokensKey: 30,
	} {
		if got, ok := attrValue(parent, key); !ok || got.AsInt64() != want {
			t.Errorf("%s = %d (present %v), want %d", key, got.AsInt64(), ok, want)
		}
	}
	child := spanNamed(t, spans, "execute_tool read")
	if child.Parent.SpanID() != parent.SpanContext.SpanID() {
		t.Errorf("tool span parent %s, want the turn span %s", child.Parent.SpanID(), parent.SpanContext.SpanID())
	}
	if got, _ := attrValue(child, semconv.GenAIToolNameKey); got.AsString() != "read" {
		t.Errorf("gen_ai.tool.name = %q, want read", got.AsString())
	}
	if got, _ := attrValue(child, semconv.GenAIToolCallIDKey); got.AsString() != "c1" {
		t.Errorf("gen_ai.tool.call.id = %q, want c1", got.AsString())
	}
	if !child.StartTime.Equal(at(0).Add(time.Second)) || !child.EndTime.Equal(at(0).Add(2*time.Second)) {
		t.Errorf("tool span timestamps %v..%v do not match the recorded call", child.StartTime, child.EndTime)
	}
}

func TestExportOmitsTokenCountsThatAreNotReported(t *testing.T) {
	exporter, memory := recordingExporter(t)
	exporter.Export(context.Background(), Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted, InputTokens: 0, OutputTokens: -4, CacheReadTokens: -1, CacheCreationTokens: 0})
	parent := spanNamed(t, memory.GetSpans(), "invoke_agent alpha")
	for _, key := range []attribute.Key{
		semconv.GenAIUsageInputTokensKey,
		semconv.GenAIUsageOutputTokensKey,
		semconv.GenAIUsageCacheReadInputTokensKey,
		semconv.GenAIUsageCacheCreationInputTokensKey,
	} {
		if _, ok := attrValue(parent, key); ok {
			t.Errorf("%s exported although the count is not reported", key)
		}
	}
}

func TestExportFailedCallMarksErrorWithBoundedMessage(t *testing.T) {
	exporter, memory := recordingExporter(t)
	exporter.Export(context.Background(), Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted, Calls: []timeline.Call{
		{ID: "c1", Tool: "bash", State: contracts.ToolCallFailed, Error: "exit 2", StartedAt: at(0), EndedAt: ptrTime(at(0).Add(time.Second))},
	}})
	child := spanNamed(t, memory.GetSpans(), "execute_tool bash")
	if child.Status.Code != codes.Error || child.Status.Description != "exit 2" {
		t.Errorf("status = %v %q, want error with the call error", child.Status.Code, child.Status.Description)
	}
	if got, _ := attrValue(child, semconv.ErrorTypeKey); got.AsString() != "_OTHER" {
		t.Errorf("error.type = %q, want _OTHER", got.AsString())
	}
}

func TestFailedCallWithoutMessageUsesFixedText(t *testing.T) {
	exporter, memory := recordingExporter(t)
	exporter.Export(context.Background(), Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted, Calls: []timeline.Call{
		{ID: "c1", Tool: "bash", State: contracts.ToolCallFailed, StartedAt: at(0), EndedAt: ptrTime(at(0).Add(time.Second))},
	}})
	child := spanNamed(t, memory.GetSpans(), "execute_tool bash")
	if child.Status.Code != codes.Error || child.Status.Description != messageToolFailed {
		t.Errorf("status = %v %q, want error %q", child.Status.Code, child.Status.Description, messageToolFailed)
	}
}

func TestFailedCallMessageIsBoundedByTheTimeline(t *testing.T) {
	var recorder timeline.Recorder
	long := strings.Repeat("x", timeline.MaxFieldBytes*3)
	started := contracts.Event{T: eventwire.EventStatus, ToolCall: &contracts.ToolCall{ID: "c1", Tool: "bash", State: contracts.ToolCallStarted, At: at(0)}}
	failed := contracts.Event{T: eventwire.EventToolDone, ToolCall: &contracts.ToolCall{ID: "c1", State: contracts.ToolCallFailed, Error: long, At: at(0).Add(time.Second)}}
	if _, _, err := recorder.Apply(started); err != nil {
		t.Fatal(err)
	}
	if _, _, err := recorder.Apply(failed); err != nil {
		t.Fatal(err)
	}
	exporter, memory := recordingExporter(t)
	exporter.Export(context.Background(), Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted, Calls: recorder.Calls()})
	child := spanNamed(t, memory.GetSpans(), "execute_tool bash")
	if len(child.Status.Description) > timeline.MaxFieldBytes {
		t.Errorf("error description is %d bytes, want at most %d", len(child.Status.Description), timeline.MaxFieldBytes)
	}
}

func TestUnfinishedCallIsClosedAtTurnEndWithError(t *testing.T) {
	exporter, memory := recordingExporter(t)
	exporter.Export(context.Background(), Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted, Calls: []timeline.Call{
		{ID: "c1", Tool: "bash", State: contracts.ToolCallStarted, StartedAt: at(0).Add(time.Second)},
	}})
	child := spanNamed(t, memory.GetSpans(), "execute_tool bash")
	if !child.EndTime.Equal(at(10 * time.Second)) {
		t.Errorf("unfinished call ends at %v, want the turn end %v", child.EndTime, at(10*time.Second))
	}
	if child.Status.Code != codes.Error || child.Status.Description != messageUnfinished {
		t.Errorf("status = %v %q, want error %q", child.Status.Code, child.Status.Description, messageUnfinished)
	}
}

func TestAbandonedTurnIsAnErrorSpan(t *testing.T) {
	exporter, memory := recordingExporter(t)
	exporter.Export(context.Background(), Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeAbandoned})
	parent := spanNamed(t, memory.GetSpans(), "invoke_agent alpha")
	if parent.Status.Code != codes.Error || parent.Status.Description != messageAbandoned {
		t.Errorf("status = %v %q, want error %q", parent.Status.Code, parent.Status.Description, messageAbandoned)
	}
}

func TestCompletedTurnIsNotAnError(t *testing.T) {
	exporter, memory := recordingExporter(t)
	exporter.Export(context.Background(), Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted})
	parent := spanNamed(t, memory.GetSpans(), "invoke_agent alpha")
	if parent.Status.Code == codes.Error {
		t.Errorf("completed turn marked as error: %q", parent.Status.Description)
	}
}

func TestExportNeverCarriesToolDetail(t *testing.T) {
	const secret = "SECRET-PROMPT-TEXT"
	exporter, memory := recordingExporter(t)
	exporter.Export(context.Background(), Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted, Calls: []timeline.Call{
		{ID: "c1", Tool: "read", Detail: secret, State: contracts.ToolCallSucceeded, StartedAt: at(0), EndedAt: ptrTime(at(0).Add(time.Second))},
	}})
	for _, span := range memory.GetSpans() {
		for _, kv := range span.Attributes {
			if strings.Contains(kv.Value.String(), secret) {
				t.Errorf("attribute %s carries the tool detail", kv.Key)
			}
		}
		if strings.Contains(span.Name, secret) || strings.Contains(span.Status.Description, secret) {
			t.Errorf("span %q carries the tool detail", span.Name)
		}
	}
}

func TestDisabledExporterExportsNothing(t *testing.T) {
	var exporter Exporter
	exporter.Export(context.Background(), Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeAbandoned})
	if exporter.Enabled() {
		t.Fatal("zero Exporter reports enabled")
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
