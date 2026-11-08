package health_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/health"
)

func stateCheck(name string, state contracts.HealthState) health.Check {
	return health.Check{Group: health.GroupBackends, Name: name, Report: contracts.ProbeReport{State: state}}
}

func TestGroupRoundTripsThroughText(t *testing.T) {
	for _, group := range health.Groups() {
		text, err := group.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%d): %v", group, err)
		}
		var back health.Group
		if err := back.UnmarshalText(text); err != nil || back != group {
			t.Fatalf("UnmarshalText(%q) = %v, %v, want %v", text, back, err, group)
		}
	}
}

func TestUnknownGroupIsRefused(t *testing.T) {
	if _, err := health.ParseGroup("network"); !errors.Is(err, health.ErrUnknownGroup) {
		t.Fatalf("ParseGroup(network) = %v, want ErrUnknownGroup", err)
	}
	if _, err := health.Group(0).MarshalText(); !errors.Is(err, health.ErrUnknownGroup) {
		t.Fatalf("Group(0).MarshalText = %v, want ErrUnknownGroup", err)
	}
}

func TestSummaryCountsEveryStateAndKeepsTheWorst(t *testing.T) {
	summary := health.Summarize([]health.Check{
		stateCheck("a", contracts.HealthOK),
		stateCheck("b", contracts.HealthOK),
		stateCheck("c", contracts.HealthDegraded),
		stateCheck("d", contracts.HealthOff),
	})
	if summary.Count(contracts.HealthOK) != 2 || summary.Count(contracts.HealthDegraded) != 1 || summary.Count(contracts.HealthOff) != 1 {
		t.Fatalf("counts = %v, want 2 ok, 1 degraded, 1 off", summary.Counts)
	}
	if summary.Worst != contracts.HealthDegraded || summary.Watched() != 3 {
		t.Fatalf("worst = %v, watched = %d, want degraded over 3", summary.Worst, summary.Watched())
	}
}

func TestReportWithoutStateCountsAsDown(t *testing.T) {
	summary := health.Summarize([]health.Check{stateCheck("a", 0), stateCheck("b", contracts.HealthOK)})
	if summary.Count(contracts.HealthDown) != 1 || summary.Worst != contracts.HealthDown {
		t.Fatalf("summary = %+v, want the stateless report down", summary)
	}
}

func TestExitCodeFollowsTheWorstState(t *testing.T) {
	cases := []struct {
		name   string
		states []contracts.HealthState
		want   health.ExitCode
	}{
		{"nothing", nil, health.ExitHealthy},
		{"only off", []contracts.HealthState{contracts.HealthOff}, health.ExitHealthy},
		{"ok and off", []contracts.HealthState{contracts.HealthOK, contracts.HealthOff}, health.ExitHealthy},
		{"degraded", []contracts.HealthState{contracts.HealthOK, contracts.HealthDegraded}, health.ExitDegraded},
		{"down beats degraded", []contracts.HealthState{contracts.HealthDegraded, contracts.HealthDown}, health.ExitDown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checks := make([]health.Check, 0, len(tc.states))
			for _, state := range tc.states {
				checks = append(checks, stateCheck("x", state))
			}
			if got := health.Summarize(checks).ExitCode(); got != tc.want {
				t.Fatalf("ExitCode = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestServiceNamesTheAccountAfterTheVendor(t *testing.T) {
	native := health.Check{Name: "claude"}
	pro := health.Check{Name: "claude", Account: "pro"}
	if native.Service() != "claude" || pro.Service() != "claude:pro" {
		t.Fatalf("services = %q, %q", native.Service(), pro.Service())
	}
	snapshot := health.Snapshot{Checks: []health.Check{native, pro, {Name: "jev"}}}
	if got := snapshot.Find("claude"); len(got) != 2 {
		t.Fatalf("Find(claude) = %d checks, want both accounts", len(got))
	}
	if got := snapshot.Find("Claude:Pro"); len(got) != 1 || got[0].Account != "pro" {
		t.Fatalf("Find(Claude:Pro) = %+v, want the pro account", got)
	}
}

func TestSnapshotSurvivesTheWireWithItsNoticeArguments(t *testing.T) {
	started := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	snapshot := health.Snapshot{
		Daemon:   health.Daemon{PID: 48213, Started: started, Socket: "/run/host.sock"},
		ProbedAt: started.Add(time.Hour),
		Elapsed:  1800 * time.Millisecond,
		Checks: []health.Check{
			{
				Group: health.GroupCompaction, Name: "jev",
				Report: contracts.ProbeReport{
					State:  contracts.HealthDown,
					Detail: contracts.Notice{Code: contracts.CodeHealthEnvMissing, Args: []contracts.Arg{contracts.Text("Name", "TYPESAFE_API_KEY")}},
					Remedy: contracts.Notice{Code: contracts.CodeHealthRemedyEnvRestart, Args: []contracts.Arg{contracts.Text("Name", "TYPESAFE_API_KEY")}},
				},
			},
			{
				Group: health.GroupKeys, Name: "anthropic",
				Report: contracts.ProbeReport{State: contracts.HealthDegraded},
				Facts:  health.Facts{Pool: &health.Pool{Keys: 3, Usable: 2, LastStatus: 429, CoolingFor: 4 * time.Minute}},
			},
			{Group: health.GroupBackends, Name: "claude", Account: "pro", Default: true, Report: contracts.ProbeReport{State: contracts.HealthOK, Latency: 900 * time.Millisecond}},
		},
	}
	data, err := health.Encode(snapshot)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := health.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if back.Daemon != snapshot.Daemon || !back.ProbedAt.Equal(snapshot.ProbedAt) || back.Elapsed != snapshot.Elapsed {
		t.Fatalf("daemon = %+v at %v in %v", back.Daemon, back.ProbedAt, back.Elapsed)
	}
	jev := back.Checks[0]
	text, err := contracts.NewRenderer().Render(i18n.French, jev.Report.Detail)
	if err != nil || !strings.Contains(text, "TYPESAFE_API_KEY") {
		t.Fatalf("detail = %q, %v, want the variable name to survive", text, err)
	}
	if jev.Report.Remedy.Code != contracts.CodeHealthRemedyEnvRestart {
		t.Fatalf("remedy = %+v", jev.Report.Remedy)
	}
	if pool := back.Checks[1].Facts.Pool; pool == nil || *pool != *snapshot.Checks[1].Facts.Pool || pool.Cooling() != 1 {
		t.Fatalf("pool = %+v", pool)
	}
	pro := back.Checks[2]
	if pro.Account != "pro" || !pro.Default || pro.Report.Latency != 900*time.Millisecond {
		t.Fatalf("pro = %+v", pro)
	}
}

func TestStatelessReportGoesOverTheWireDown(t *testing.T) {
	data, err := health.Encode(health.Snapshot{Checks: []health.Check{stateCheck("a", 0)}})
	if err != nil {
		t.Fatalf("Encode of a stateless report: %v", err)
	}
	back, err := health.Decode(data)
	if err != nil || back.Checks[0].Report.State != contracts.HealthDown {
		t.Fatalf("Decode = %+v, %v, want the report down", back, err)
	}
}

func TestGarbledAnswerIsMalformed(t *testing.T) {
	if _, err := health.Decode([]byte(`{"checks":[{"group":"nowhere"}]}`)); !errors.Is(err, health.ErrMalformed) {
		t.Fatalf("Decode = %v, want ErrMalformed", err)
	}
}

func fixed(state contracts.HealthState) func(context.Context, bool) contracts.ProbeReport {
	return func(context.Context, bool) contracts.ProbeReport { return contracts.ProbeReport{State: state} }
}

func TestRunnerKeepsTheProbeOrder(t *testing.T) {
	probes := []health.Probe{
		{Check: health.Check{Name: "a"}, Run: fixed(contracts.HealthOK)},
		{Check: health.Check{Name: "b"}, Run: fixed(contracts.HealthDegraded)},
		{Check: health.Check{Name: "c"}, Run: fixed(0)},
	}
	checks, _ := health.Runner{Workers: 2}.Run(context.Background(), probes, false)
	got := []contracts.HealthState{checks[0].Report.State, checks[1].Report.State, checks[2].Report.State}
	want := []contracts.HealthState{contracts.HealthOK, contracts.HealthDegraded, contracts.HealthDown}
	if !slices.Equal(got, want) || checks[1].Name != "b" {
		t.Fatalf("states = %v, want %v in probe order", got, want)
	}
}

func TestRunnerNeverRunsMoreProbesThanItsWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var running, peak atomic.Int32
		slow := func(_ context.Context, _ bool) contracts.ProbeReport {
			now := running.Add(1)
			for {
				old := peak.Load()
				if now <= old || peak.CompareAndSwap(old, now) {
					break
				}
			}
			time.Sleep(time.Second)
			running.Add(-1)
			return contracts.ProbeReport{State: contracts.HealthOK}
		}
		probes := make([]health.Probe, 7)
		for index := range probes {
			probes[index] = health.Probe{Check: health.Check{Name: "p"}, Run: slow}
		}
		checks, elapsed := health.Runner{Workers: 3, Timeout: time.Minute}.Run(context.Background(), probes, false)
		if peak.Load() != 3 || len(checks) != 7 {
			t.Fatalf("peak = %d over %d checks, want 3 at a time", peak.Load(), len(checks))
		}
		if elapsed != 3*time.Second {
			t.Fatalf("elapsed = %v, want three rounds of one second", elapsed)
		}
	})
}

func stalls(ctx context.Context, _ bool) contracts.ProbeReport {
	<-ctx.Done()
	return contracts.ProbeReport{State: contracts.HealthOK, Remedy: contracts.Notice{Code: contracts.CodeHealthRemedyRun, Args: []contracts.Arg{contracts.Text("Command", "claude login")}}}
}

func TestProbePastItsTimeoutIsDown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := health.Runner{Timeout: 2 * time.Second, DeepTimeout: 9 * time.Second}
		checks, elapsed := runner.Run(context.Background(), []health.Probe{{Check: health.Check{Name: "slow"}, Run: stalls}}, false)
		check := checks[0]
		if check.Report.State != contracts.HealthDown || check.Facts.TimedOut != 2*time.Second || elapsed != 2*time.Second {
			t.Fatalf("check = %+v after %v, want down after the 2 s timeout", check, elapsed)
		}
		if check.Report.Remedy.Code != contracts.CodeHealthRemedyRun {
			t.Fatalf("remedy = %+v, want the probe remedy kept", check.Report.Remedy)
		}
	})
}

func TestDeepRunWaitsForTheDeepTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := health.Runner{Timeout: 2 * time.Second, DeepTimeout: 9 * time.Second}
		checks, elapsed := runner.Run(context.Background(), []health.Probe{{Check: health.Check{Name: "slow"}, Run: stalls}}, true)
		if checks[0].Facts.TimedOut != 9*time.Second || elapsed != 9*time.Second {
			t.Fatalf("timed out = %v after %v, want the deep timeout", checks[0].Facts.TimedOut, elapsed)
		}
	})
}

func TestDeepFlagReachesTheProbe(t *testing.T) {
	var deep atomic.Bool
	probe := health.Probe{Run: func(_ context.Context, asked bool) contracts.ProbeReport {
		deep.Store(asked)
		return contracts.ProbeReport{State: contracts.HealthOK}
	}}
	health.Runner{}.Run(context.Background(), []health.Probe{probe}, true)
	if !deep.Load() {
		t.Fatal("the probe was not asked for a deep check")
	}
}

func TestCancelledRunMarksTheWaitingProbesDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checks, _ := health.Runner{Workers: 1}.Run(ctx, []health.Probe{
		{Check: health.Check{Name: "a"}, Run: fixed(contracts.HealthOK)},
		{Check: health.Check{Name: "b"}, Run: fixed(contracts.HealthOK)},
	}, false)
	for _, check := range checks {
		if check.Report.State != contracts.HealthDown {
			t.Fatalf("check %s = %v, want down once the run is cancelled", check.Name, check.Report.State)
		}
	}
}
