package host

import (
	"context"
	"errors"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
	"github.com/Herrscherd/herrscher/core/keyproxy"
)

func TestPoolStateFollowsTheUsableKeys(t *testing.T) {
	tests := []struct {
		name string
		pool keyproxy.PoolState
		want contracts.HealthState
	}{
		{name: "no key configured", pool: keyproxy.PoolState{Keys: 0}, want: contracts.HealthOff},
		{name: "every key usable", pool: keyproxy.PoolState{Keys: 2, Usable: 2}, want: contracts.HealthOK},
		{name: "some keys cooling", pool: keyproxy.PoolState{Keys: 3, Usable: 1}, want: contracts.HealthDegraded},
		{name: "no key usable", pool: keyproxy.PoolState{Keys: 2, Usable: 0}, want: contracts.HealthDown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := poolState(tt.pool); got != tt.want {
				t.Errorf("poolState() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDaemonProbeIsOKWithItsSocket(t *testing.T) {
	probe := daemonProbe(health.Daemon{PID: 42, Socket: "/run/herrscher.sock"})
	if probe.Check.Group != health.GroupCore {
		t.Errorf("group = %v, want %v", probe.Check.Group, health.GroupCore)
	}
	if probe.Check.Facts.Socket != "/run/herrscher.sock" {
		t.Errorf("socket = %q, want %q", probe.Check.Facts.Socket, "/run/herrscher.sock")
	}
	if got := probe.Run(context.Background(), false); got.State != contracts.HealthOK {
		t.Errorf("state = %v, want %v", got.State, contracts.HealthOK)
	}
}

func TestGatewayRunReportsLiveOrOff(t *testing.T) {
	tests := []struct {
		name string
		live bool
		want contracts.HealthState
	}{
		{name: "running instance", live: true, want: contracts.HealthOK},
		{name: "no instance", live: false, want: contracts.HealthOff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := gatewayRun(contracts.Plugin{}, tt.live)
			if got := run(context.Background(), false); got.State != tt.want {
				t.Errorf("state = %v, want %v", got.State, tt.want)
			}
		})
	}
}

func TestGatewayRunDelegatesToThePluginProbe(t *testing.T) {
	var got contracts.ProbeAccess
	plugin := contracts.Plugin{Probe: func(_ context.Context, access contracts.ProbeAccess) contracts.ProbeReport {
		got = access
		return contracts.ProbeReport{State: contracts.HealthDegraded}
	}}
	run := gatewayRun(plugin, true)
	report := run(context.Background(), true)
	if report.State != contracts.HealthDegraded {
		t.Errorf("state = %v, want %v", report.State, contracts.HealthDegraded)
	}
	if !got.Deep {
		t.Errorf("access.Deep = false, want true")
	}
}

func TestMemoryRunReportsLiveOrOff(t *testing.T) {
	tests := []struct {
		name string
		live bool
		want contracts.HealthState
	}{
		{name: "instantiated memory", live: true, want: contracts.HealthOK},
		{name: "memory not instantiated", live: false, want: contracts.HealthOff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := memoryRun(contracts.Plugin{}, tt.live)
			if got := run(context.Background(), false); got.State != tt.want {
				t.Errorf("state = %v, want %v", got.State, tt.want)
			}
		})
	}
}

func TestMemoryRunDelegatesToThePluginProbe(t *testing.T) {
	plugin := contracts.Plugin{Probe: func(context.Context, contracts.ProbeAccess) contracts.ProbeReport {
		return contracts.ProbeReport{State: contracts.HealthDown}
	}}
	run := memoryRun(plugin, false)
	if got := run(context.Background(), false); got.State != contracts.HealthDown {
		t.Errorf("state = %v, want %v", got.State, contracts.HealthDown)
	}
}

func TestBackendRunHandsTheProfileEnvToThePluginProbe(t *testing.T) {
	const name = "HEALTH_TEST_PROFILE_HOME"
	t.Setenv(name, "from the host")
	env := map[string]string{name: "from the profile"}
	var got contracts.ProbeAccess
	plugin := contracts.Plugin{Probe: func(_ context.Context, access contracts.ProbeAccess) contracts.ProbeReport {
		got = access
		return contracts.ProbeReport{State: contracts.HealthOK}
	}}
	run := backendRun(plugin, "work", env)
	if report := run(context.Background(), false); report.State != contracts.HealthOK {
		t.Errorf("state = %v, want %v", report.State, contracts.HealthOK)
	}
	if got.Account != "work" {
		t.Errorf("access.Account = %q, want %q", got.Account, "work")
	}
	if value := got.Getenv(name); value != "from the profile" {
		t.Errorf("Getenv(%s) = %q, want %q", name, value, "from the profile")
	}
	if value := got.Getenv("HEALTH_TEST_UNSET_VARIABLE"); value != "" {
		t.Errorf("Getenv(unset) = %q, want empty", value)
	}
}

func TestCompactionRunIsOKWithoutAPlugin(t *testing.T) {
	run := compactionRun("herrscher-missing-compactor")
	if got := run(context.Background(), false); got.State != contracts.HealthOK {
		t.Errorf("state = %v, want %v", got.State, contracts.HealthOK)
	}
}

func TestHealthSourceRefusesBeforeItIsBound(t *testing.T) {
	var unbound healthSource
	if _, err := unbound.run(context.Background(), false); !errors.Is(err, errHealthNotReady) {
		t.Errorf("run() error = %v, want %v", err, errHealthNotReady)
	}
	var missing *healthSource
	if _, err := missing.run(context.Background(), false); !errors.Is(err, errHealthNotReady) {
		t.Errorf("nil run() error = %v, want %v", err, errHealthNotReady)
	}
}
