package otelexport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"

	"github.com/Herrscherd/herrscher/core/timeline"
)

const deadEndpoint = "http://127.0.0.1:1"

type traceRecorder struct {
	coltrace.UnimplementedTraceServiceServer

	received *atomic.Int32
}

func (r traceRecorder) Export(_ context.Context, _ *coltrace.ExportTraceServiceRequest) (*coltrace.ExportTraceServiceResponse, error) {
	r.received.Add(1)
	return &coltrace.ExportTraceServiceResponse{}, nil
}

func listenLiteral(t *testing.T, host string) (net.Listener, string) {
	t.Helper()
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Skipf("loopback %s unavailable: %v", host, err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close listener: %v", err)
		}
	})
	return listener, listener.Addr().String()
}

func startHTTPCollector(t *testing.T, host string) (string, *atomic.Int32) {
	t.Helper()
	listener, addr := listenLiteral(t, host)
	var received atomic.Int32
	collector := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/traces" {
			received.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	collector.Listener = listener
	collector.Start()
	t.Cleanup(collector.Close)
	return addr, &received
}

func startGRPCCollector(t *testing.T, host string) (string, *atomic.Int32) {
	t.Helper()
	listener, addr := listenLiteral(t, host)
	var received atomic.Int32
	server := grpc.NewServer()
	coltrace.RegisterTraceServiceServer(server, traceRecorder{received: &received})
	go func() {
		if err := server.Serve(listener); err != nil {
			t.Errorf("serve grpc collector: %v", err)
		}
	}()
	t.Cleanup(server.Stop)
	return addr, &received
}

func TestExportReachesCollectorAtIPLiteral(t *testing.T) {
	t.Setenv(envEndpoint, deadEndpoint)
	t.Setenv(envTracesEndpoint, deadEndpoint)
	t.Setenv(envInsecure, "false")
	t.Setenv(envTracesInsecure, "false")

	for _, tc := range []struct {
		name     string
		host     string
		protocol string
		start    func(*testing.T, string) (string, *atomic.Int32)
		settings func(addr string) map[string]string
	}{
		{
			name: "http IPv4 base endpoint", host: "127.0.0.1", protocol: "http/protobuf", start: startHTTPCollector,
			settings: func(addr string) map[string]string { return map[string]string{envEndpoint: "http://" + addr} },
		},
		{
			name: "http IPv6 base endpoint", host: "::1", protocol: "http/protobuf", start: startHTTPCollector,
			settings: func(addr string) map[string]string { return map[string]string{envEndpoint: "http://" + addr} },
		},
		{
			name: "http IPv6 traces endpoint", host: "::1", protocol: "http/protobuf", start: startHTTPCollector,
			settings: func(addr string) map[string]string {
				return map[string]string{envTracesEndpoint: "http://" + addr + "/v1/traces"}
			},
		},
		{
			name: "grpc IPv4 host and port, plaintext", host: "127.0.0.1", protocol: "grpc", start: startGRPCCollector,
			settings: func(addr string) map[string]string {
				return map[string]string{envEndpoint: addr, envInsecure: "true"}
			},
		},
		{
			name: "grpc IPv6 host and port, plaintext", host: "::1", protocol: "grpc", start: startGRPCCollector,
			settings: func(addr string) map[string]string {
				return map[string]string{envEndpoint: addr, envTracesInsecure: "true"}
			},
		},
		{
			name: "grpc IPv6 URL", host: "::1", protocol: "grpc", start: startGRPCCollector,
			settings: func(addr string) map[string]string { return map[string]string{envEndpoint: "http://" + addr} },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr, received := tc.start(t, tc.host)
			values := tc.settings(addr)
			values[envProtocol] = tc.protocol
			exporter, err := Setup(context.Background(), environment(values))
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
			if received.Load() == 0 {
				t.Fatalf("no export reached the collector at %s (%s); the SDK read the decoy environment instead of the lookup", addr, strconv.Quote(tc.name))
			}
		})
	}
}
