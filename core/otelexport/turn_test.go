package otelexport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	contracts "github.com/Herrscherd/herrscher-contracts"
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

func mustExport(t *testing.T, exporter Exporter, turn Turn) {
	t.Helper()
	if err := exporter.Export(context.Background(), turn); err != nil {
		t.Fatal(err)
	}
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

func errorTypeOf(t *testing.T, span tracetest.SpanStub) string {
	t.Helper()
	got, ok := attrValue(span, semconv.ErrorTypeKey)
	if !ok {
		return ""
	}
	return got.AsString()
}

func TestExportTurnWithTwoCallsCarriesGenAIAttributes(t *testing.T) {
	exporter, memory := recordingExporter(t)
	mustExport(t, exporter, Turn{
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
		semconv.GenAIUsageInputTokensKey:              1050,
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

func TestInputTokensCountOnlyTheCountsReported(t *testing.T) {
	exporter, memory := recordingExporter(t)
	mustExport(t, exporter, Turn{Session: "alpha", Started: at(0), Ended: at(time.Second), Outcome: OutcomeCompleted, CacheReadTokens: 900, CacheCreationTokens: -3})
	parent := spanNamed(t, memory.GetSpans(), "invoke_agent alpha")
	if got, ok := attrValue(parent, semconv.GenAIUsageInputTokensKey); !ok || got.AsInt64() != 900 {
		t.Errorf("input tokens = %d (present %v), want the reported cache read 900", got.AsInt64(), ok)
	}
}

func TestExportOmitsTokenCountsThatAreNotReported(t *testing.T) {
	exporter, memory := recordingExporter(t)
	mustExport(t, exporter, Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted, InputTokens: 0, OutputTokens: -4, CacheReadTokens: -1, CacheCreationTokens: 0})
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

func TestFailedCallMarksToolFailedWithoutItsErrorText(t *testing.T) {
	const detail = "exit 2 with SECRET-ARGUMENT"
	exporter, memory := recordingExporter(t)
	mustExport(t, exporter, Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted, Calls: []timeline.Call{
		{ID: "c1", Tool: "bash", State: contracts.ToolCallFailed, Error: detail, StartedAt: at(0), EndedAt: ptrTime(at(0).Add(time.Second))},
	}})
	child := spanNamed(t, memory.GetSpans(), "execute_tool bash")
	if child.Status.Code != codes.Error || child.Status.Description != "" {
		t.Errorf("status = %v %q, want error with no description", child.Status.Code, child.Status.Description)
	}
	if got := errorTypeOf(t, child); got != errorTypeToolFailed {
		t.Errorf("error.type = %q, want %q", got, errorTypeToolFailed)
	}
	for _, kv := range child.Attributes {
		if strings.Contains(kv.Value.String(), "SECRET-ARGUMENT") {
			t.Errorf("attribute %s carries the tool error text", kv.Key)
		}
	}
}

func TestToolNameAndIDAreBoundedByTheTimeline(t *testing.T) {
	long := strings.Repeat("x", timeline.MaxFieldBytes*2)
	exporter, memory := recordingExporter(t)
	mustExport(t, exporter, Turn{Session: "alpha", Model: long, Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted, Calls: []timeline.Call{
		{ID: long, Tool: long, State: contracts.ToolCallSucceeded, StartedAt: at(0), EndedAt: ptrTime(at(time.Second))},
	}})
	for _, span := range memory.GetSpans() {
		for _, kv := range span.Attributes {
			if len(kv.Value.String()) > timeline.MaxFieldBytes {
				t.Errorf("attribute %s is %d bytes, want at most %d", kv.Key, len(kv.Value.String()), timeline.MaxFieldBytes)
			}
		}
		if len(span.Name) > timeline.MaxFieldBytes {
			t.Errorf("span name is %d bytes, want at most %d", len(span.Name), timeline.MaxFieldBytes)
		}
	}
}

func TestUnfinishedCallIsClosedAtTurnEndWithError(t *testing.T) {
	exporter, memory := recordingExporter(t)
	mustExport(t, exporter, Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted, Calls: []timeline.Call{
		{ID: "c1", Tool: "bash", State: contracts.ToolCallStarted, StartedAt: at(0).Add(time.Second)},
	}})
	child := spanNamed(t, memory.GetSpans(), "execute_tool bash")
	if !child.EndTime.Equal(at(10 * time.Second)) {
		t.Errorf("unfinished call ends at %v, want the turn end %v", child.EndTime, at(10*time.Second))
	}
	if child.Status.Code != codes.Error {
		t.Errorf("status = %v, want error", child.Status.Code)
	}
	if got := errorTypeOf(t, child); got != errorTypeToolUnfinished {
		t.Errorf("error.type = %q, want %q", got, errorTypeToolUnfinished)
	}
}

func TestFailedTurnIsAnErrorWithItsOwnType(t *testing.T) {
	exporter, memory := recordingExporter(t)
	mustExport(t, exporter, Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeFailed})
	parent := spanNamed(t, memory.GetSpans(), "invoke_agent alpha")
	if parent.Status.Code != codes.Error {
		t.Errorf("failed turn status = %v, want error", parent.Status.Code)
	}
	if got := errorTypeOf(t, parent); got != errorTypeFailed {
		t.Errorf("error.type = %q, want %q", got, errorTypeFailed)
	}
}

func TestInterruptedTurnIsAnErrorWithItsOwnType(t *testing.T) {
	exporter, memory := recordingExporter(t)
	mustExport(t, exporter, Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeInterrupted})
	parent := spanNamed(t, memory.GetSpans(), "invoke_agent alpha")
	if parent.Status.Code != codes.Error {
		t.Errorf("interrupted turn status = %v, want error", parent.Status.Code)
	}
	if got := errorTypeOf(t, parent); got != errorTypeInterrupted {
		t.Errorf("error.type = %q, want %q", got, errorTypeInterrupted)
	}
}

func TestAbandonedTurnIsAnErrorWithItsOwnType(t *testing.T) {
	exporter, memory := recordingExporter(t)
	mustExport(t, exporter, Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeAbandoned})
	parent := spanNamed(t, memory.GetSpans(), "invoke_agent alpha")
	if parent.Status.Code != codes.Error {
		t.Errorf("abandoned turn status = %v, want error", parent.Status.Code)
	}
	if got := errorTypeOf(t, parent); got != errorTypeAbandoned {
		t.Errorf("error.type = %q, want %q", got, errorTypeAbandoned)
	}
}

func TestCompletedTurnIsNotAnError(t *testing.T) {
	exporter, memory := recordingExporter(t)
	mustExport(t, exporter, Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted})
	parent := spanNamed(t, memory.GetSpans(), "invoke_agent alpha")
	if parent.Status.Code == codes.Error {
		t.Errorf("completed turn marked as error")
	}
}

func TestZeroOutcomeIsRefusedWithoutASpan(t *testing.T) {
	exporter, memory := recordingExporter(t)
	err := exporter.Export(context.Background(), Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second)})
	if !errors.Is(err, ErrUnknownOutcome) {
		t.Fatalf("zero outcome error = %v, want ErrUnknownOutcome", err)
	}
	if n := len(memory.GetSpans()); n != 0 {
		t.Errorf("zero outcome exported %d spans", n)
	}
}

func TestExportNeverCarriesToolDetail(t *testing.T) {
	const secret = "SECRET-PROMPT-TEXT"
	exporter, memory := recordingExporter(t)
	mustExport(t, exporter, Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeCompleted, Calls: []timeline.Call{
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
	if err := exporter.Export(context.Background(), Turn{Session: "alpha", Started: at(0), Ended: at(10 * time.Second), Outcome: OutcomeAbandoned}); err != nil {
		t.Fatal(err)
	}
	if exporter.Enabled() {
		t.Fatal("zero Exporter reports enabled")
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
