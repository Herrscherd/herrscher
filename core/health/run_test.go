package health_test

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
)

func TestProbeThatNeverReturnsIsDownAtItsTimeout(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	blocked := func(context.Context, bool) contracts.ProbeReport {
		<-release
		return contracts.ProbeReport{State: contracts.HealthOK}
	}
	runner := health.Runner{Timeout: 20 * time.Millisecond}
	checks, _ := runner.Run(context.Background(), []health.Probe{{Check: health.Check{Name: "blocked"}, Run: blocked}}, false)
	if checks[0].Report.State != contracts.HealthDown || checks[0].Facts.TimedOut != 20*time.Millisecond {
		t.Fatalf("check = %+v, want down at the 20 ms timeout", checks[0])
	}
}

func TestPanickingProbeIsDown(t *testing.T) {
	panicking := func(context.Context, bool) contracts.ProbeReport {
		panic("secret value")
	}
	checks, _ := health.Runner{}.Run(context.Background(), []health.Probe{{Check: health.Check{Name: "boom"}, Run: panicking}}, false)
	if checks[0].Report.State != contracts.HealthDown {
		t.Fatalf("state = %v, want down after a panic", checks[0].Report.State)
	}
}

func TestLatencyIsTimedFromTheRunnerClockWhenTheProbeLeavesItZero(t *testing.T) {
	ticks := 0
	clock := func() time.Time {
		ticks++
		return time.Unix(0, int64(ticks)*int64(time.Millisecond))
	}
	probe := func(context.Context, bool) contracts.ProbeReport {
		return contracts.ProbeReport{State: contracts.HealthOK}
	}
	checks, _ := health.Runner{Now: clock}.Run(context.Background(), []health.Probe{{Check: health.Check{Name: "fast"}, Run: probe}}, false)
	if checks[0].Report.Latency <= 0 {
		t.Fatalf("latency = %v, want a positive latency from the runner clock", checks[0].Report.Latency)
	}
}
