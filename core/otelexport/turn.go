package otelexport

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/timeline"
)

// Outcome says how a turn ended.
type Outcome int

// The outcomes a turn can end with.
const (
	OutcomeCompleted Outcome = iota + 1
	OutcomeAbandoned
)

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
	operationInvokeAgent = "invoke_agent"
	operationExecuteTool = "execute_tool"
	messageAbandoned     = "turn abandoned"
	messageUnfinished    = "tool call unfinished at the end of the turn"
	messageToolFailed    = "tool call failed"
)

// Export sends the turn as one invoke_agent span with an execute_tool child per call. It only queues the spans, so it never waits for the collector. A disabled exporter does nothing.
func (e Exporter) Export(ctx context.Context, turn Turn) {
	if !e.Enabled() {
		return
	}
	ctx, span := e.tracer.Start(ctx, spanName(operationInvokeAgent, turn.Session), trace.WithSpanKind(trace.SpanKindClient), trace.WithTimestamp(turn.Started), trace.WithAttributes(turnAttributes(turn)...))
	for _, call := range turn.Calls {
		e.exportCall(ctx, call, turn.Ended)
	}
	if turn.Outcome == OutcomeAbandoned {
		fail(span, messageAbandoned)
	}
	span.End(trace.WithTimestamp(turn.Ended))
}

func (e Exporter) exportCall(parent context.Context, call timeline.Call, turnEnd time.Time) {
	_, span := e.tracer.Start(parent, spanName(operationExecuteTool, call.Tool), trace.WithSpanKind(trace.SpanKindInternal), trace.WithTimestamp(call.StartedAt), trace.WithAttributes(toolAttributes(call)...))
	end := turnEnd
	if call.Finished() {
		end = *call.EndedAt
	}
	switch {
	case !call.Finished():
		fail(span, messageUnfinished)
	case call.State == contracts.ToolCallFailed:
		fail(span, failureMessage(call.Error))
	}
	span.End(trace.WithTimestamp(end))
}

func fail(span trace.Span, message string) {
	span.SetStatus(codes.Error, message)
	span.SetAttributes(semconv.ErrorTypeOther)
}

func failureMessage(text string) string {
	if text == "" {
		return messageToolFailed
	}
	return text
}

func spanName(operation, subject string) string {
	if subject == "" {
		return operation
	}
	return operation + " " + subject
}

func turnAttributes(turn Turn) []attribute.KeyValue {
	attrs := []attribute.KeyValue{semconv.GenAIOperationNameKey.String(operationInvokeAgent)}
	attrs = appendText(attrs, semconv.GenAIProviderNameKey, turn.Provider)
	attrs = appendText(attrs, semconv.GenAIAgentNameKey, turn.Session)
	attrs = appendText(attrs, semconv.GenAIRequestModelKey, turn.Model)
	attrs = appendCount(attrs, semconv.GenAIUsageInputTokensKey, turn.InputTokens)
	attrs = appendCount(attrs, semconv.GenAIUsageOutputTokensKey, turn.OutputTokens)
	attrs = appendCount(attrs, semconv.GenAIUsageCacheReadInputTokensKey, turn.CacheReadTokens)
	return appendCount(attrs, semconv.GenAIUsageCacheCreationInputTokensKey, turn.CacheCreationTokens)
}

func toolAttributes(call timeline.Call) []attribute.KeyValue {
	attrs := []attribute.KeyValue{semconv.GenAIOperationNameKey.String(operationExecuteTool)}
	attrs = appendText(attrs, semconv.GenAIToolNameKey, call.Tool)
	return appendText(attrs, semconv.GenAIToolCallIDKey, call.ID)
}

func appendText(attrs []attribute.KeyValue, key attribute.Key, value string) []attribute.KeyValue {
	if value == "" {
		return attrs
	}
	return append(attrs, key.String(value))
}

func appendCount(attrs []attribute.KeyValue, key attribute.Key, count int) []attribute.KeyValue {
	if !timeline.ReportsCount(count) {
		return attrs
	}
	return append(attrs, key.Int(count))
}
