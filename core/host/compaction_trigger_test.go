package host

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/compactwire"
)

const triggerLimit = 200_000

func triggerNames() []string {
	return []string{"jev", contracts.CompactStageBackend}
}

type stageCompactor struct {
	calls *atomic.Int32
	after int
}

func (c stageCompactor) Compact(_ context.Context, _ contracts.CompactRequest, _ func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	c.calls.Add(1)
	return contracts.CompactResult{TokensAfter: c.after, TokensMeasured: true}, nil
}

type triggeredStage struct {
	stageCompactor

	percent int
}

func (s triggeredStage) TriggerPercent() int {
	return s.percent
}

func triggerDriver(host, trigger int) *sessionDriver {
	policy := compactPolicy(host, triggerNames()...)
	policy.triggers = make(map[string]int)
	if trigger != 0 {
		policy.triggers["jev"] = trigger
	}
	d := newSessionDriver("s1", nil, nil, nil)
	d.compaction = policy
	return d
}

func registerTriggerCompactor(t *testing.T, percent int) {
	t.Helper()
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "jev", Category: contracts.CategoryCompactor},
		Compactor: func(context.Context, contracts.PluginConfig) (contracts.Compactor, error) {
			return triggeredStage{percent: percent}, nil
		},
	})
}

func TestAutomaticRunPicksTheStagesDueAtTheUsage(t *testing.T) {
	tests := []struct {
		name    string
		host    int
		trigger int
		used    int
		want    []string
		full    bool
	}{
		{name: "own threshold below the host runs the early stage alone", host: 80, trigger: 50, used: 100_000, want: []string{"jev"}},
		{name: "host threshold runs the whole stack", host: 80, trigger: 50, used: 160_000, want: triggerNames(), full: true},
		{name: "under the early threshold nothing runs", host: 80, trigger: 50, used: 90_000},
		{name: "zero trigger means the host threshold", host: 80, trigger: 0, used: 100_000},
		{name: "zero trigger runs the whole stack at the host threshold", host: 80, trigger: 0, used: 160_000, want: triggerNames(), full: true},
		{name: "trigger above the host does not delay the stage", host: 80, trigger: 90, used: 100_000},
		{name: "trigger above the host runs at the host threshold", host: 80, trigger: 90, used: 160_000, want: triggerNames(), full: true},
		{name: "host zero never runs automatically", host: 0, trigger: 50, used: 190_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := triggerDriver(tt.host, tt.trigger)
			run, due := d.automaticRun(tt.used, triggerLimit)
			if due != (len(tt.want) > 0) || run.full != tt.full || !slices.Equal(run.names, tt.want) {
				t.Fatalf("automaticRun(%d) = %+v, due %v; want %v full %v", tt.used, run, due, tt.want, tt.full)
			}
		})
	}
}

func TestEarlyStageRunsOncePerCrossingAndRearms(t *testing.T) {
	steps := []struct {
		used, after int
		want        []string
		full        bool
	}{
		{used: 100_000, after: 110_000, want: []string{"jev"}},
		{used: 110_000},
		{used: 90_000},
		{used: 100_000, after: 60_000, want: []string{"jev"}},
		{used: 170_000, after: 170_000, want: triggerNames(), full: true},
		{used: 175_000},
		{used: 90_000},
		{used: 100_000, after: 60_000, want: []string{"jev"}},
		{used: 170_000, after: 20_000, want: triggerNames(), full: true},
	}
	d := triggerDriver(80, 50)
	for index, step := range steps {
		run, due := d.automaticRun(step.used, triggerLimit)
		if due != (len(step.want) > 0) || run.full != step.full || !slices.Equal(run.names, step.want) {
			t.Fatalf("step %d: automaticRun(%d) = %+v, due %v; want %v full %v", index, step.used, run, due, step.want, step.full)
		}
		if !due {
			continue
		}
		d.markSpent(run)
		d.settleCompaction(contracts.CompactResult{TokensAfter: step.after, TokensMeasured: true}, triggerLimit, run)
	}
}

func TestStackWithoutTriggerKeepsTheHostThreshold(t *testing.T) {
	tests := []struct {
		used int
		want []string
		full bool
	}{
		{used: 100_000},
		{used: 160_000, want: []string{contracts.CompactStageBackend}, full: true},
	}
	for _, tt := range tests {
		d := newSessionDriver("s1", nil, nil, nil)
		d.compaction = compactPolicy(80)
		run, due := d.automaticRun(tt.used, triggerLimit)
		if due != (len(tt.want) > 0) || run.full != tt.full || !slices.Equal(run.names, tt.want) {
			t.Fatalf("automaticRun(%d) = %+v, due %v; want %v full %v", tt.used, run, due, tt.want, tt.full)
		}
	}
}

func TestStageTriggerIsReadAndValidatedAtStart(t *testing.T) {
	for _, percent := range []int{-1, 101} {
		t.Run("refused "+strconv.Itoa(percent), func(t *testing.T) {
			registerTriggerCompactor(t, percent)
			_, err := newCompactionPolicy(context.Background(), nil, triggerNames(), nil)
			if !errors.Is(err, contracts.ErrStageTriggerPercentOutOfRange) {
				t.Fatalf("newCompactionPolicy = %v, want ErrStageTriggerPercentOutOfRange", err)
			}
			if !strings.Contains(err.Error(), "jev") || !strings.Contains(err.Error(), strconv.Itoa(percent)) {
				t.Fatalf("error = %q, want it to name the stage and the value %d", err, percent)
			}
		})
	}
	for _, percent := range []int{0, 50, 100} {
		t.Run("accepted "+strconv.Itoa(percent), func(t *testing.T) {
			registerTriggerCompactor(t, percent)
			policy, err := newCompactionPolicy(context.Background(), nil, triggerNames(), nil)
			if err != nil {
				t.Fatalf("newCompactionPolicy: %v", err)
			}
			if policy.triggers["jev"] != percent {
				t.Fatalf("triggers = %v, want jev at %d", policy.triggers, percent)
			}
		})
	}
}

func TestEarlyStageRunsBeforeTheBackendAndNotAgain(t *testing.T) {
	var calls atomic.Int32
	policy := compactPolicy(80, triggerNames()...)
	policy.triggers = map[string]int{"jev": 50}
	policy.plugins["jev"] = stageCompactor{calls: &calls, after: 110_000}
	f := newCompactFixture(t, "claude", policy)

	openTurn(t, f, 100_000)
	expectNoFrame(t, f)
	openTurn(t, f, 105_000)
	if got := calls.Load(); got != 1 {
		t.Fatalf("early stage ran %d times, want once while usage stayed above its threshold", got)
	}
	openTurn(t, f, 170_000)
	if !compactFrameOrQuiet(t, f, measuredAnswer(t, 20_000)) {
		t.Fatal("the host threshold did not run the backend stage")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("early stage ran %d times, want 2 once the whole stack ran at the host threshold", got)
	}
}

func TestManualCompactionRunsTheWholeStack(t *testing.T) {
	var calls atomic.Int32
	policy := compactPolicy(80, triggerNames()...)
	policy.triggers = map[string]int{"jev": 50}
	policy.plugins["jev"] = stageCompactor{calls: &calls, after: 10_000}
	f := newCompactFixture(t, "claude", policy)

	f.driver.requestCompaction("", contracts.Conversation{})
	if got := nextFrame(t, f); got.T != compactwire.EventRequest {
		t.Fatalf("frame = %+v, want the backend stage of the manual compaction", got)
	}
	f.from <- measuredAnswer(t, 4_000)
	waitEmitted(t, f, eventCompactDone)
	if got := calls.Load(); got != 1 {
		t.Fatalf("early stage ran %d times in a manual compaction, want once", got)
	}
}
