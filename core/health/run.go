package health

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Probe is one check to run: the check it fills in, and the function that
// probes the service. Run should return when its context is done.
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
// with. A probe that does not return by its timeout is abandoned, and its
// goroutine leaks with it: a probe must honour its context.
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
			checks[index] = r.runOne(ctx, probe, deep, timeout)
		})
	}
	wg.Wait()
	return checks, now().Sub(started)
}

func (r Runner) runOne(ctx context.Context, probe Probe, deep bool, timeout time.Duration) Check {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	clock := r.clock()
	started := clock()
	answer := make(chan contracts.ProbeReport, 1)
	go answerProbe(probeCtx, probe, deep, answer)
	select {
	case report := <-answer:
		if report.Latency == 0 {
			report.Latency = clock().Sub(started)
		}
		if probeCtx.Err() != nil || ctx.Err() != nil {
			return timedOut(probe.Check, report, timeout)
		}
		check := probe.Check
		check.Report = Settled(report)
		return check
	case <-probeCtx.Done():
		return timedOut(probe.Check, contracts.ProbeReport{}, timeout)
	}
}

func answerProbe(ctx context.Context, probe Probe, deep bool, answer chan<- contracts.ProbeReport) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("health probe panicked", "check", probe.Check.Service(), "panic_type", fmt.Sprintf("%T", recovered))
			answer <- contracts.ProbeReport{State: contracts.HealthDown}
		}
	}()
	answer <- probe.Run(ctx, deep)
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
