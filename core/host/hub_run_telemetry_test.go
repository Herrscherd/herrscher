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
			startTelemetry(context.Background(), newHostMessages(), i18n.English, slog.New(slog.NewTextHandler(&logs, nil)))
			if !strings.Contains(logs.String(), tc.want) {
				t.Fatalf("log = %q, want the refusal %q", logs.String(), tc.want)
			}
		})
	}
}
