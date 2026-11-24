package host

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/config"
)

func loadConfigFile(t *testing.T, body string) config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

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

func TestTelemetryTurnedOffInConfigFileExportsNothing(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	cfg := loadConfigFile(t, `{"telemetry":{"enabled":false}}`)
	var logs bytes.Buffer
	telemetry, stop := startTelemetryForConfig(context.Background(), newHostMessages(), i18n.English, slog.New(slog.NewTextHandler(&logs, nil)), cfg)
	defer stop()
	if telemetry.Enabled() {
		t.Fatal("the exporter is on although the configuration turns telemetry off")
	}
	const want = "turn telemetry is off: the configuration sets telemetry.enabled to false"
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("log = %q, want the configuration notice %q", logs.String(), want)
	}
}

func TestTelemetryTurnedOffInConfigFileSkipsAnInvalidEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "ftp://collector")
	cfg := loadConfigFile(t, `{"telemetry":{"enabled":false}}`)
	var logs bytes.Buffer
	telemetry, stop := startTelemetryForConfig(context.Background(), newHostMessages(), i18n.English, slog.New(slog.NewTextHandler(&logs, nil)), cfg)
	defer stop()
	if telemetry.Enabled() {
		t.Fatal("the exporter is on although the configuration turns telemetry off")
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("log = %q, want no warning when the configuration turns telemetry off", logs.String())
	}
}

func TestTelemetryWithoutConfigSwitchKeepsTheEnvironmentDecision(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	cfg := loadConfigFile(t, `{"telemetry":{}}`)
	var logs bytes.Buffer
	telemetry, stop := startTelemetryForConfig(context.Background(), newHostMessages(), i18n.English, slog.New(slog.NewTextHandler(&logs, nil)), cfg)
	defer stop()
	if !telemetry.Enabled() {
		t.Fatalf("the exporter is off with a valid endpoint and no configuration switch, log = %q", logs.String())
	}
}
