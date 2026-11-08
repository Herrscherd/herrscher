package health

import (
	"context"
	"errors"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Probe is one check to run: the check it fills in, and the function that
// probes the service. Run stops when its context is done.
type Probe struct {
	Check Check
	Run   func(ctx context.Context, deep bool) contracts.ProbeReport
}

// The defaults a [Runner] falls back to when a field is zero.
const (
	DefaultWorkers     = 8
	DefaultTimeout     = 5 * time.Second
	DefaultDeepTimeout = 30 * time.Second
)

// Runner runs probes side by side, at most Workers at a time, each one bounded
// by Timeout, or DeepTimeout for a deep run. Now is the clock the run is timed
// with.
type Runner struct {
	Workers     int
	Timeout     time.Duration
	DeepTimeout time.Duration
	Now         func() time.Time
}

// Run runs every probe and returns the checks in the order of probes, with how
// long the run took. A probe that outlives its timeout, or that is still
// waiting for a worker when ctx is done, is down.
func (r Runner) Run(ctx context.Context, probes []Probe, deep bool) ([]Check, time.Duration) {
	now := r.clock()
	started := now()
	timeout := r.timeout(deep)
	checks := make([]Check, len(probes))
	slots := make(chan struct{}, r.workers())
	var wg sync.WaitGroup
	for index := range probes {
		probe := probes[index]
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			checks[index] = timedOut(probe.Check, contracts.ProbeReport{}, timeout)
			continue
		}
		wg.Go(func() {
			defer func() { <-slots }()
			checks[index] = runOne(ctx, probe, deep, timeout)
		})
	}
	wg.Wait()
	return checks, now().Sub(started)
}

func runOne(ctx context.Context, probe Probe, deep bool, timeout time.Duration) Check {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	report := probe.Run(probeCtx, deep)
	if errors.Is(probeCtx.Err(), context.DeadlineExceeded) || ctx.Err() != nil {
		return timedOut(probe.Check, report, timeout)
	}
	check := probe.Check
	check.Report = Settled(report)
	return check
}

func timedOut(check Check, report contracts.ProbeReport, timeout time.Duration) Check {
	check.Report = contracts.ProbeReport{State: contracts.HealthDown, Latency: timeout, Remedy: report.Remedy}
	check.Facts.TimedOut = timeout
	return check
}

func (r Runner) clock() func() time.Time {
	if r.Now != nil {
		return r.Now
	}
	return time.Now
}

func (r Runner) timeout(deep bool) time.Duration {
	if deep {
		if r.DeepTimeout > 0 {
			return r.DeepTimeout
		}
		return DefaultDeepTimeout
	}
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultTimeout
}

func (r Runner) workers() int {
	if r.Workers > 0 {
		return r.Workers
	}
	return DefaultWorkers
}
