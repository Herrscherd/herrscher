package otelexport

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/timeline"
)

// Outcome says how a turn ended. Its zero value is not an outcome, and Export refuses it.
type Outcome int

// The outcomes a turn can end with.
const (
	outcomeUnset Outcome = iota
	OutcomeCompleted
	OutcomeFailed
	OutcomeInterrupted
	OutcomeAbandoned
)

// ErrUnknownOutcome is returned by Export for a turn whose Outcome is not one of the outcomes above.
var ErrUnknownOutcome = errors.New("otelexport: unknown turn outcome")

// Turn is one settled turn of a session, as the host saw it. Only the fields below leave the process: the prompt, the reply and each call's detail and arguments never do.
type Turn struct {
	Session             string
	Provider            string
	Model               string
	Started             time.Time
	Ended               time.Time
	Outcome             Outcome
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	Calls               []timeline.Call
}

const (
	errorTypeFailed         = "turn_failed"
	errorTypeAbandoned      = "turn_abandoned"
	errorTypeToolFailed     = "tool_failed"
	errorTypeToolUnfinished = "tool_unfinished"
)

const (
	turnOutcomeKey         = attribute.Key("herrscher.turn.outcome")
	turnOutcomeInterrupted = "interrupted"
)

// Export sends the turn as one invoke_agent span with an execute_tool child per call. It only queues the spans, so it never waits for the collector. A disabled exporter does nothing and reports no error.
func (e Exporter) Export(ctx context.Context, turn Turn) error {
	if !e.Enabled() {
		return nil
	}
	if err := checkOutcome(turn.Outcome); err != nil {
		return err
	}
	ctx, span := e.tracer.Start(ctx, spanName(semconv.GenAIOperationNameInvokeAgent, turn.Session), trace.WithSpanKind(trace.SpanKindClient), trace.WithTimestamp(turn.Started), trace.WithAttributes(turnAttributes(turn)...))
	for _, call := range turn.Calls {
		e.exportCall(ctx, call, turn.Ended)
	}
	markOutcome(span, turn.Outcome)
	span.End(trace.WithTimestamp(turn.Ended))
	return nil
}

func checkOutcome(outcome Outcome) error {
	switch outcome {
	case OutcomeCompleted, OutcomeFailed, OutcomeInterrupted, OutcomeAbandoned:
		return nil
	case outcomeUnset:
	}
	return ErrUnknownOutcome
}

func markOutcome(span trace.Span, outcome Outcome) {
	switch outcome {
	case OutcomeFailed:
		fail(span, errorTypeFailed)
	case OutcomeInterrupted:
		span.SetAttributes(turnOutcomeKey.String(turnOutcomeInterrupted))
	case OutcomeAbandoned:
		fail(span, errorTypeAbandoned)
	case OutcomeCompleted, outcomeUnset:
	}
}

func (e Exporter) exportCall(parent context.Context, call timeline.Call, turnEnd time.Time) {
	_, span := e.tracer.Start(parent, spanName(semconv.GenAIOperationNameExecuteTool, call.Tool), trace.WithSpanKind(trace.SpanKindInternal), trace.WithTimestamp(call.StartedAt), trace.WithAttributes(toolAttributes(call)...))
	end := turnEnd
	if call.Finished() {
		end = *call.EndedAt
	}
	switch {
	case !call.Finished():
		fail(span, errorTypeToolUnfinished)
	case call.State == contracts.ToolCallFailed:
		fail(span, errorTypeToolFailed)
	}
	span.End(trace.WithTimestamp(end))
}

func fail(span trace.Span, errorType string) {
	span.SetStatus(codes.Error, "")
	span.SetAttributes(semconv.ErrorTypeKey.String(errorType))
}

func spanName(operation attribute.KeyValue, subject string) string {
	name := operation.Value.AsString()
	if subject == "" {
		return name
	}
	return timeline.BoundField(name + " " + subject)
}

func turnAttributes(turn Turn) []attribute.KeyValue {
	attrs := []attribute.KeyValue{semconv.GenAIOperationNameInvokeAgent}
	attrs = appendText(attrs, semconv.GenAIProviderNameKey, turn.Provider)
	attrs = appendText(attrs, semconv.GenAIAgentNameKey, turn.Session)
	attrs = appendText(attrs, semconv.GenAIRequestModelKey, turn.Model)
	if input, reported := inputTokens(turn); reported {
		attrs = append(attrs, semconv.GenAIUsageInputTokensKey.Int(input))
	}
	attrs = appendCount(attrs, semconv.GenAIUsageOutputTokensKey, turn.OutputTokens)
	attrs = appendCount(attrs, semconv.GenAIUsageCacheReadInputTokensKey, turn.CacheReadTokens)
	return appendCount(attrs, semconv.GenAIUsageCacheCreationInputTokensKey, turn.CacheCreationTokens)
}

func inputTokens(turn Turn) (int, bool) {
	total, reported := 0, false
	for _, count := range [...]int{turn.InputTokens, turn.CacheReadTokens, turn.CacheCreationTokens} {
		if timeline.ReportsCount(count) {
			total += count
			reported = true
		}
	}
	return total, reported
}

func toolAttributes(call timeline.Call) []attribute.KeyValue {
	attrs := []attribute.KeyValue{semconv.GenAIOperationNameExecuteTool}
	attrs = appendText(attrs, semconv.GenAIToolNameKey, call.Tool)
	return appendText(attrs, semconv.GenAIToolCallIDKey, call.ID)
}

func appendText(attrs []attribute.KeyValue, key attribute.Key, value string) []attribute.KeyValue {
	if value == "" {
		return attrs
	}
	return append(attrs, key.String(timeline.BoundField(value)))
}

func appendCount(attrs []attribute.KeyValue, key attribute.Key, count int) []attribute.KeyValue {
	if !timeline.ReportsCount(count) {
		return attrs
	}
	return append(attrs, key.Int(count))
}
