package otelexport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"github.com/Herrscherd/herrscher/core/timeline"
)

func environment(values map[string]string) Lookup {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestSetupWithoutEndpointStaysDisabled(t *testing.T) {
	exporter, err := Setup(context.Background(), environment(nil))
	if err != nil {
		t.Fatal(err)
	}
	if exporter.Enabled() {
		t.Fatal("exporter enabled without any OTLP endpoint")
	}
}

func TestSDKDisabledOverridesEndpoint(t *testing.T) {
	exporter, err := Setup(context.Background(), environment(map[string]string{
		envEndpoint:    "http://127.0.0.1:1",
		envSDKDisabled: "TRUE",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if exporter.Enabled() {
		t.Fatal("OTEL_SDK_DISABLED=true did not disable the exporter")
	}
}

func TestTracesEndpointEnablesExporter(t *testing.T) {
	exporter, err := Setup(context.Background(), environment(map[string]string{envTracesEndpoint: "http://127.0.0.1:1"}))
	if err != nil {
		t.Fatal(err)
	}
	if !exporter.Enabled() {
		t.Fatal("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT alone did not enable the exporter")
	}
	if err := exporter.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestUnsupportedProtocolIsAnError(t *testing.T) {
	_, err := Setup(context.Background(), environment(map[string]string{
		envEndpoint:       "http://127.0.0.1:1",
		envTracesProtocol: "carrier-pigeon",
	}))
	if !errors.Is(err, ErrUnsupportedProtocol) {
		t.Fatalf("err = %v, want ErrUnsupportedProtocol", err)
	}
}

func TestServiceNameDefaultsToHerrscher(t *testing.T) {
	res, err := resourceFrom(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := res.Set().Value(semconv.ServiceNameKey); got.AsString() != defaultServiceName {
		t.Errorf("service.name = %q, want %q", got.AsString(), defaultServiceName)
	}
}

func TestServiceNameEnvironmentOverridesDefault(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "custom-name")
	res, err := resourceFrom(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := res.Set().Value(semconv.ServiceNameKey); got.AsString() != "custom-name" {
		t.Errorf("service.name = %q, want custom-name from OTEL_SERVICE_NAME", got.AsString())
	}
}

func TestShutdownFlushesTurnsToCollector(t *testing.T) {
	var posts atomic.Int32
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/traces" {
			posts.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	t.Setenv(envEndpoint, collector.URL)
	exporter, err := Setup(context.Background(), os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	mustExport(t, exporter, Turn{Session: "alpha", Started: started, Ended: started.Add(time.Second), Outcome: OutcomeCompleted, Calls: []timeline.Call{
		{ID: "c1", Tool: "read", StartedAt: started, EndedAt: ptrTime(started.Add(time.Millisecond))},
	}})
	if err := exporter.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts.Load() == 0 {
		t.Fatal("no trace request reached the collector after Shutdown")
	}
}

func TestShutdownIsBoundedWhenCollectorHangs(t *testing.T) {
	release := make(chan struct{})
	collector := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(collector.Close)
	t.Cleanup(func() { close(release) })
	t.Setenv(envEndpoint, collector.URL)
	exporter, err := Setup(context.Background(), os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	exporter.grace = 200 * time.Millisecond
	mustExport(t, exporter, Turn{Session: "alpha", Started: time.Now(), Ended: time.Now(), Outcome: OutcomeCompleted})
	started := time.Now()
	if err := exporter.Shutdown(context.Background()); err != nil {
		t.Log(err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Shutdown took %v with a hanging collector, want it bounded by the grace", elapsed)
	}
}

func TestShutdownOfDisabledExporterIsNoop(t *testing.T) {
	var exporter Exporter
	if err := exporter.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidEndpointIsRefused(t *testing.T) {
	for _, endpoint := range []string{"::bad", "http://[::1", "not a url", "ftp://x", "http://"} {
		_, err := Setup(context.Background(), environment(map[string]string{envEndpoint: endpoint}))
		if !errors.Is(err, ErrInvalidEndpoint) {
			t.Errorf("endpoint %q: err = %v, want ErrInvalidEndpoint", endpoint, err)
		}
	}
}
