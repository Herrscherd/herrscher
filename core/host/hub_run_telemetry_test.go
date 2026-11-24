package host

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestTelemetryRefusalNamesItsCause(t *testing.T) {
	for _, tc := range []struct {
		name     string
		endpoint string
		protocol string
		want     string
	}{
		{"unsupported protocol", "http://127.0.0.1:4318", "carrier-pigeon", "turn telemetry is off: the OTLP protocol is not one herrscher supports"},
		{"invalid endpoint", "ftp://collector", "", "turn telemetry is off: the OTLP endpoint is not an address its protocol accepts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tc.endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", tc.protocol)
			var logs bytes.Buffer
			startTelemetry(context.Background(), newHostMessages(), i18n.English, slog.New(slog.NewTextHandler(&logs, nil)), false)
			if !strings.Contains(logs.String(), tc.want) {
				t.Fatalf("log = %q, want the refusal %q", logs.String(), tc.want)
			}
		})
	}
}

func TestTelemetryTurnedOffByConfigExportsNothing(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	var logs bytes.Buffer
	telemetry, stop := startTelemetry(context.Background(), newHostMessages(), i18n.English, slog.New(slog.NewTextHandler(&logs, nil)), true)
	defer stop()
	if telemetry.Enabled() {
		t.Fatal("the exporter is on although the configuration turns telemetry off")
	}
	const want = "turn telemetry is off: the configuration sets telemetry.enabled to false"
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("log = %q, want the configuration notice %q", logs.String(), want)
	}
}

func TestTelemetryTurnedOffByConfigSkipsAnInvalidEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "ftp://collector")
	var logs bytes.Buffer
	telemetry, stop := startTelemetry(context.Background(), newHostMessages(), i18n.English, slog.New(slog.NewTextHandler(&logs, nil)), true)
	defer stop()
	if telemetry.Enabled() {
		t.Fatal("the exporter is on although the configuration turns telemetry off")
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("log = %q, want no warning when the configuration turns telemetry off", logs.String())
	}
}

func TestTelemetryNotTurnedOffByConfigKeepsTheEnvironmentDecision(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	var logs bytes.Buffer
	telemetry, stop := startTelemetry(context.Background(), newHostMessages(), i18n.English, slog.New(slog.NewTextHandler(&logs, nil)), false)
	defer stop()
	if !telemetry.Enabled() {
		t.Fatalf("the exporter is off with a valid endpoint and no configuration switch, log = %q", logs.String())
	}
}
