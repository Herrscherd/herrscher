// Package otelexport sends each turn of a session to an OpenTelemetry collector as an invoke_agent span with one execute_tool child per tool call, following the GenAI semantic conventions of go.opentelemetry.io/otel/semconv/v1.41.0. It is off unless an OTLP endpoint is configured through the standard OTEL_* variables, and it exports no prompt, reply, tool detail or argument.
package otelexport

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	envSDKDisabled     = "OTEL_SDK_DISABLED"
	envEndpoint        = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envTracesEndpoint  = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	envProtocol        = "OTEL_EXPORTER_OTLP_PROTOCOL"
	envTracesProtocol  = "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"
	defaultServiceName = "herrscher"
	tracerName         = "github.com/Herrscherd/herrscher/core/otelexport"
	shutdownGrace      = 5 * time.Second
)

// ErrUnsupportedProtocol is returned by Setup when the configured OTLP traces protocol is neither grpc nor http/protobuf.
var ErrUnsupportedProtocol = errors.New("otelexport: unsupported OTLP traces protocol")

// ErrInvalidEndpoint is returned by Setup when the configured OTLP endpoint is not an http or https URL with a host.
var ErrInvalidEndpoint = errors.New("otelexport: invalid OTLP endpoint")

// Lookup reads one environment variable, the way os.LookupEnv does.
type Lookup func(key string) (string, bool)

// Exporter sends turns to a collector. Its zero value is the disabled exporter: it holds no provider, opens no connection and does nothing.
type Exporter struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	grace    time.Duration
}

type protocol int

const (
	protocolHTTP protocol = iota + 1
	protocolGRPC
)

// Setup returns an enabled Exporter when an OTLP endpoint is configured and OTEL_SDK_DISABLED is not "true", and the disabled zero Exporter otherwise. A configured endpoint or protocol that is invalid is an error, matched with ErrInvalidEndpoint or ErrUnsupportedProtocol. The OTLP client reads the rest of its configuration (headers, timeouts, batch size) from the standard OTEL_* variables, and it connects lazily, so an unreachable collector does not delay Setup.
func Setup(ctx context.Context, lookup Lookup) (Exporter, error) {
	if !enabled(lookup) {
		return Exporter{}, nil
	}
	endpoint := firstSet(lookup, envTracesEndpoint, envEndpoint)
	if err := validateEndpoint(endpoint); err != nil {
		return Exporter{}, err
	}
	proto, err := parseProtocol(firstSet(lookup, envTracesProtocol, envProtocol))
	if err != nil {
		return Exporter{}, err
	}
	res, err := resourceFrom(ctx)
	if err != nil {
		return Exporter{}, fmt.Errorf("otelexport: resource: %w", err)
	}
	spans, err := newSpanExporter(ctx, proto)
	if err != nil {
		return Exporter{}, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(spans), sdktrace.WithResource(res))
	return newExporter(provider), nil
}

// Enabled reports whether turns are exported.
func (e Exporter) Enabled() bool {
	return e.provider != nil
}

// Shutdown flushes the queued spans and stops the provider, waiting at most a few seconds even when the collector does not answer. It stays bounded when the daemon context is already cancelled, which is the normal case at shutdown.
func (e Exporter) Shutdown(ctx context.Context) error {
	if !e.Enabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.grace)
	defer cancel()
	if err := e.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("otelexport: shutdown: %w", err)
	}
	return nil
}

func newExporter(provider *sdktrace.TracerProvider) Exporter {
	return Exporter{provider: provider, tracer: provider.Tracer(tracerName), grace: shutdownGrace}
}

func enabled(lookup Lookup) bool {
	if value, _ := lookup(envSDKDisabled); strings.EqualFold(value, "true") {
		return false
	}
	return firstSet(lookup, envTracesEndpoint, envEndpoint) != ""
}

func validateEndpoint(raw string) error {
	endpoint, err := url.Parse(raw)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return ErrInvalidEndpoint
	}
	return nil
}

func parseProtocol(value string) (protocol, error) {
	switch value {
	case "", "http/protobuf":
		return protocolHTTP, nil
	case "grpc":
		return protocolGRPC, nil
	}
	return 0, fmt.Errorf("%w: %q", ErrUnsupportedProtocol, value)
}

func firstSet(lookup Lookup, keys ...string) string {
	for _, key := range keys {
		if value, ok := lookup(key); ok && value != "" {
			return value
		}
	}
	return ""
}

func resourceFrom(ctx context.Context) (*resource.Resource, error) {
	return resource.New(ctx, resource.WithAttributes(semconv.ServiceName(defaultServiceName)), resource.WithFromEnv(), resource.WithTelemetrySDK())
}

func newSpanExporter(ctx context.Context, proto protocol) (sdktrace.SpanExporter, error) {
	switch proto {
	case protocolGRPC:
		exporter, err := otlptracegrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("otelexport: grpc exporter: %w", err)
		}
		return exporter, nil
	case protocolHTTP:
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("otelexport: http exporter: %w", err)
		}
		return exporter, nil
	}
	return nil, ErrUnsupportedProtocol
}
