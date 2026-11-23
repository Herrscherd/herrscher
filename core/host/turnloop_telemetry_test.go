package host

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/otelexport"
	"github.com/Herrscherd/herrscher/core/outbox"
)

type traceCollector struct {
	mu    sync.Mutex
	spans []*tracepb.Span
}

func (c *traceCollector) record(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var request coltracepb.ExportTraceServiceRequest
	if err := proto.Unmarshal(body, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, resource := range request.GetResourceSpans() {
		for _, scope := range resource.GetScopeSpans() {
			c.spans = append(c.spans, scope.GetSpans()...)
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (c *traceCollector) all() []*tracepb.Span {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*tracepb.Span(nil), c.spans...)
}

func (c *traceCollector) named(t *testing.T, name string) *tracepb.Span {
	t.Helper()
	for _, span := range c.all() {
		if span.GetName() == name {
			return span
		}
	}
	t.Fatalf("no span named %q among %d spans", name, len(c.all()))
	return nil
}

func spanAttr(span *tracepb.Span, key string) *commonpb.AnyValue {
	for _, kv := range span.GetAttributes() {
		if kv.GetKey() == key {
			return kv.GetValue()
		}
	}
	return nil
}

func startTelemetryCollector(t *testing.T) (*traceCollector, otelexport.Exporter) {
	t.Helper()
	collector := &traceCollector{}
	server := httptest.NewServer(http.HandlerFunc(collector.record))
	t.Cleanup(server.Close)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
	exporter, err := otelexport.Setup(context.Background(), os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	if !exporter.Enabled() {
		t.Fatal("exporter stayed disabled with an OTLP endpoint configured")
	}
	return collector, exporter
}

func flushTelemetry(t *testing.T, exporter otelexport.Exporter) {
	t.Helper()
	if err := exporter.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func telemetryDriver(exporter otelexport.Exporter, terms func() modelTerms) *sessionDriver {
	return &sessionDriver{
		messages:    newHostMessages(),
		cliMessages: clitext.NewMessages(),
		persists:    newPersistWrites(),
		name:        "alpha",
		telemetry:   exporter,
		terms:       terms,
		gate:        noBudgetGate{},
		metrics:     metrics.NewRegistry(),
	}
}

func claudeTerms() modelTerms {
	return modelTerms{vendor: vendorClaude, model: "claude-x"}
}

func TestCompletedTurnIsExportedWithUsageAndTools(t *testing.T) {
	collector, exporter := startTelemetryCollector(t)
	d := telemetryDriver(exporter, claudeTerms)
	d.turnSpan = d.openTurnSpan()
	for _, ev := range []contracts.Event{
		{T: eventwire.EventStatus, ToolCall: &contracts.ToolCall{ID: "c1", Tool: "read", State: contracts.ToolCallStarted, At: time.Now()}},
		{T: eventwire.EventToolDone, ToolCall: &contracts.ToolCall{ID: "c1", State: contracts.ToolCallSucceeded, At: time.Now()}},
	} {
		if _, _, err := d.calls.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	d.completeTurn(context.Background(), contracts.Event{T: eventwire.EventReply, Text: "done", Done: true, TokensIn: 120, Tokens: 45, CacheRead: 900, CacheCreate: 30}, time.Now(), 0, outbox.Frame{})
	flushTelemetry(t, exporter)

	turn := collector.named(t, "invoke_agent alpha")
	if turn.GetStatus().GetCode() == tracepb.Status_STATUS_CODE_ERROR {
		t.Errorf("completed turn exported as an error")
	}
	if got := spanAttr(turn, "gen_ai.usage.input_tokens"); got.GetIntValue() != 1050 {
		t.Errorf("gen_ai.usage.input_tokens = %d, want 1050", got.GetIntValue())
	}
	collector.named(t, "execute_tool read")
}

func TestAbandonedTurnIsExportedAsErrorWithItsType(t *testing.T) {
	collector, exporter := startTelemetryCollector(t)
	d := telemetryDriver(exporter, claudeTerms)
	d.turnSpan = d.openTurnSpan()
	d.abandon(context.Background(), contracts.Event{T: eventwire.EventInput})
	flushTelemetry(t, exporter)

	turn := collector.named(t, "invoke_agent alpha")
	if turn.GetStatus().GetCode() != tracepb.Status_STATUS_CODE_ERROR {
		t.Errorf("abandoned turn status = %v, want error", turn.GetStatus().GetCode())
	}
	if got := spanAttr(turn, "error.type"); got.GetStringValue() != "turn_abandoned" {
		t.Errorf("error.type = %q, want turn_abandoned", got.GetStringValue())
	}
}

func TestFailedTurnIsExportedAsErrorWithItsFixedType(t *testing.T) {
	collector, exporter := startTelemetryCollector(t)
	d := telemetryDriver(exporter, claudeTerms)
	d.turnSpan = d.openTurnSpan()
	d.completeTurn(context.Background(), contracts.Event{T: eventwire.EventReply, Text: "partial", Done: true, Outcome: contracts.TurnFailed}, time.Now(), 0, outbox.Frame{})
	flushTelemetry(t, exporter)

	turn := collector.named(t, "invoke_agent alpha")
	if turn.GetStatus().GetCode() != tracepb.Status_STATUS_CODE_ERROR {
		t.Errorf("failed turn status = %v, want error", turn.GetStatus().GetCode())
	}
	if got := spanAttr(turn, "error.type"); got.GetStringValue() != "turn_failed" {
		t.Errorf("error.type = %q, want turn_failed", got.GetStringValue())
	}
}

func TestInterruptedTurnIsExportedWithItsOutcomeNotAsError(t *testing.T) {
	collector, exporter := startTelemetryCollector(t)
	d := telemetryDriver(exporter, claudeTerms)
	d.turnSpan = d.openTurnSpan()
	d.completeTurn(context.Background(), contracts.Event{T: eventwire.EventReply, Text: "stopped", Done: true, Outcome: contracts.TurnInterrupted}, time.Now(), 0, outbox.Frame{})
	flushTelemetry(t, exporter)

	turn := collector.named(t, "invoke_agent alpha")
	if turn.GetStatus().GetCode() == tracepb.Status_STATUS_CODE_ERROR {
		t.Errorf("interrupted turn exported as an error")
	}
	if spanAttr(turn, "error.type") != nil {
		t.Errorf("interrupted turn carries error.type, want none")
	}
	if got := spanAttr(turn, "herrscher.turn.outcome"); got.GetStringValue() != "interrupted" {
		t.Errorf("herrscher.turn.outcome = %q, want interrupted", got.GetStringValue())
	}
}

func TestTurnSpanNamesTheGenAIProviderOfEachVendor(t *testing.T) {
	collector, exporter := startTelemetryCollector(t)
	for _, tc := range []struct {
		vendor string
		want   string
	}{
		{vendorClaude, "anthropic"},
		{backendCodex, "openai"},
		{"cursor", "cursor"},
	} {
		d := telemetryDriver(exporter, func() modelTerms { return modelTerms{vendor: tc.vendor} })
		d.turnSpan = d.openTurnSpan()
		d.abandon(context.Background(), contracts.Event{T: eventwire.EventInput})
	}
	flushTelemetry(t, exporter)
	var got []string
	for _, span := range collector.all() {
		value := spanAttr(span, "gen_ai.provider.name")
		got = append(got, value.GetStringValue())
	}
	want := []string{"anthropic", "openai", "cursor"}
	if len(got) != len(want) {
		t.Fatalf("got %d providers %v, want %v", len(got), got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("turn %d provider = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestModelIsReadWhenTheTurnStarts(t *testing.T) {
	collector, exporter := startTelemetryCollector(t)
	model := "claude-before"
	d := telemetryDriver(exporter, func() modelTerms { return modelTerms{vendor: vendorClaude, model: model} })
	d.turnSpan = d.openTurnSpan()
	model = "claude-after"
	d.abandon(context.Background(), contracts.Event{T: eventwire.EventInput})
	flushTelemetry(t, exporter)

	turn := collector.named(t, "invoke_agent alpha")
	if got := spanAttr(turn, "gen_ai.request.model"); got.GetStringValue() != "claude-before" {
		t.Errorf("gen_ai.request.model = %q, want the model at turn start", got.GetStringValue())
	}
}
