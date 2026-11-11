package host

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/historywire"
)

const typesafeEnv = "TYPESAFE_API_KEY"

type historyRun struct {
	editorGiven bool
	history     contracts.History
	outcome     contracts.PruneOutcome
	err         error
}

type pruningCompactor struct {
	runs chan historyRun
}

func (c pruningCompactor) Compact(ctx context.Context, req contracts.CompactRequest, _ func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	run := historyRun{editorGiven: req.History != nil}
	if req.History == nil {
		c.runs <- run
		return contracts.CompactResult{}, nil
	}
	run.history, run.err = req.History.ReadHistory(ctx)
	if run.err == nil {
		run.outcome, run.err = req.History.PruneHistory(ctx, []contracts.HistoryEdit{{CallID: "c1", Action: contracts.PruneResult, KeepChars: 10}})
	}
	c.runs <- run
	return contracts.CompactResult{}, run.err
}

func historyFixture(t *testing.T, declared bool) (*compactFixture, chan historyRun, *[]string) {
	t.Helper()
	runs := make(chan historyRun, 1)
	policy := compactPolicy(92, "jev", contracts.CompactStageBackend)
	policy.plugins["jev"] = pruningCompactor{runs: runs}
	policy.historyEditors["claude"] = declared
	warned := new([]string)
	policy.warn = func(key string, _ map[string]any) { *warned = append(*warned, key) }
	return newCompactFixture(t, "claude", policy), runs, warned
}

func nextRun(t *testing.T, runs chan historyRun) historyRun {
	t.Helper()
	select {
	case run := <-runs:
		return run
	case <-time.After(2 * time.Second):
		t.Fatal("the compaction stage never finished")
	}
	return historyRun{}
}

func expectFrameOf(t *testing.T, f *compactFixture, kind string) contracts.Event {
	t.Helper()
	frame := nextFrame(t, f)
	if frame.T != kind {
		t.Fatalf("frame = %+v, want %s", frame, kind)
	}
	return frame
}

func TestPluginStageReadsAndPrunesTheSessionHistory(t *testing.T) {
	f, runs, _ := historyFixture(t, true)
	f.driver.requestCompaction("", contracts.Conversation{})
	read := expectFrameOf(t, f, historywire.EventRead)
	answer, err := historywire.History(contracts.History{Messages: []contracts.HistoryMessage{{Role: contracts.RoleAssistant, Calls: []contracts.HistoryCall{{ID: "c1", Tool: "Read", Result: "long"}}}}})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	f.from <- historywire.Answer(contracts.Event{Who: "stale"}, answer[0])
	for _, frame := range answer {
		f.from <- historywire.Answer(read, frame)
	}
	prune := expectFrameOf(t, f, historywire.EventPrune)
	edits, err := historywire.DecodePrune(prune)
	if err != nil || len(edits) != 1 || edits[0].CallID != "c1" {
		t.Fatalf("prune edits = %+v, %v, want the stage's edit of c1", edits, err)
	}
	pruned, err := historywire.Pruned(contracts.PruneOutcome{Edited: 1, CharsBefore: 400, CharsAfter: 10})
	if err != nil {
		t.Fatalf("Pruned: %v", err)
	}
	f.from <- historywire.Answer(prune, pruned)
	run := nextRun(t, runs)
	if run.err != nil || len(run.history.Calls()) != 1 || run.outcome.Edited != 1 || run.outcome.CharsAfter != 10 {
		t.Fatalf("stage run = %+v, want the bridge's history and outcome", run)
	}
	expectFrameOf(t, f, compactwire.EventRequest)
	f.from <- measuredAnswer(t, 20_000)
	waitEmitted(t, f, eventCompactDone)
}

func TestPluginStageGetsNoEditorWhenTheBackendDeclaresNone(t *testing.T) {
	f, runs, _ := historyFixture(t, false)
	f.driver.requestCompaction("", contracts.Conversation{})
	if run := nextRun(t, runs); run.editorGiven {
		t.Fatal("the stage received a history editor from a backend that declares none")
	}
	expectFrameOf(t, f, compactwire.EventRequest)
	f.from <- measuredAnswer(t, 20_000)
	waitEmitted(t, f, eventCompactDone)
}

func TestBackendWithoutTheHistoryPortFailsTheStageAndWarnsOnce(t *testing.T) {
	f, runs, warned := historyFixture(t, true)
	for range 2 {
		f.driver.requestCompaction("", contracts.Conversation{})
		read := expectFrameOf(t, f, historywire.EventRead)
		f.from <- historywire.Answer(read, historywire.NoPort())
		if run := nextRun(t, runs); !errors.Is(run.err, historywire.ErrNoHistoryEditor) {
			t.Fatalf("stage error = %v, want ErrNoHistoryEditor", run.err)
		}
		waitEmitted(t, f, eventCompactFailed)
	}
	if len(*warned) != 1 || (*warned)[0] != "warn.history_port_missing" {
		t.Fatalf("warnings = %q, want one line about the missing history port", *warned)
	}
}

func TestHangupDuringAHistoryReadReleasesTheStage(t *testing.T) {
	f, runs, _ := historyFixture(t, true)
	f.driver.requestCompaction("", contracts.Conversation{})
	expectFrameOf(t, f, historywire.EventRead)
	close(f.driver.hangup)
	if run := nextRun(t, runs); !errors.Is(run.err, errBridgeGone) {
		t.Fatalf("stage error = %v, want errBridgeGone", run.err)
	}
	expectNoFrame(t, f)
}

func registerSettingsCompactor(t *testing.T, factory contracts.CompactorFactory) {
	t.Helper()
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(contracts.Plugin{
		Manifest:  contracts.Manifest{Kind: "jev", Category: contracts.CategoryCompactor, Config: []contracts.Setting{{Key: "api_key", Env: typesafeEnv, Required: true}}},
		Compactor: factory,
	})
}

func keyedFactory(keys *[]string) contracts.CompactorFactory {
	return func(_ context.Context, cfg contracts.PluginConfig) (contracts.Compactor, error) {
		*keys = append(*keys, cfg.Get("api_key"))
		return pruningCompactor{}, nil
	}
}

func TestCompactorStageResolvesItsSettings(t *testing.T) {
	var keys []string
	registerSettingsCompactor(t, keyedFactory(&keys))
	t.Setenv(typesafeEnv, "sk-test")
	policy, err := newCompactionPolicy(context.Background(), testLanguage(), nil, []string{"jev", contracts.CompactStageBackend}, nil)
	if err != nil {
		t.Fatalf("newCompactionPolicy: %v", err)
	}
	if policy.plugins["jev"] == nil || len(keys) != 1 || keys[0] != "sk-test" {
		t.Fatalf("plugins = %v, keys = %v, want the jev stage built with the key", policy.plugins, keys)
	}
}

func TestMissingCompactorSettingRefusesStartup(t *testing.T) {
	var keys []string
	registerSettingsCompactor(t, keyedFactory(&keys))
	t.Setenv(typesafeEnv, "")
	_, err := newCompactionPolicy(context.Background(), testLanguage(), nil, []string{"jev", contracts.CompactStageBackend}, nil)
	if !errors.Is(err, errCompactorSettings) {
		t.Fatalf("newCompactionPolicy = %v, want errCompactorSettings", err)
	}
	if _, ok := errors.AsType[*contracts.MissingSettingsError](err); !ok {
		t.Fatalf("newCompactionPolicy = %v, want a MissingSettingsError inside", err)
	}
	if !strings.Contains(err.Error(), typesafeEnv) || !strings.Contains(err.Error(), "jev") {
		t.Fatalf("error = %q, want it to name the stage and %s", err, typesafeEnv)
	}
	if len(keys) != 0 {
		t.Fatal("the factory ran although a required setting is missing")
	}
}

func TestStackWithoutTheCompactorNeedsNoSetting(t *testing.T) {
	var keys []string
	registerSettingsCompactor(t, keyedFactory(&keys))
	t.Setenv(typesafeEnv, "")
	if _, err := newCompactionPolicy(context.Background(), testLanguage(), nil, nil, nil); err != nil {
		t.Fatalf("newCompactionPolicy without jev in the stack = %v, want a clean start without %s", err, typesafeEnv)
	}
	if len(keys) != 0 {
		t.Fatal("a compactor absent from the stack was built")
	}
}

func stagelessFactory(context.Context, contracts.PluginConfig) (contracts.Compactor, error) {
	var stage contracts.Compactor
	return stage, nil
}

func TestCustomStageRefusesBrokenPlugins(t *testing.T) {
	cases := []struct {
		name    string
		factory contracts.CompactorFactory
		stage   string
		want    error
	}{
		{"no factory", nil, "jev", errNoCompactorFactory},
		{"nil stage", stagelessFactory, "jev", errNilCompactor},
		{"unknown name", keyedFactory(new([]string)), "ghost", errNoCompactorPlugin},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registerSettingsCompactor(t, tc.factory)
			getenv := func(string) string { return "sk-test" }
			if _, err := customStage(context.Background(), tc.stage, getenv); !errors.Is(err, tc.want) {
				t.Fatalf("customStage = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPruneSendsTheEditsInBatchesAndSumsTheOutcome(t *testing.T) {
	f, _, _ := historyFixture(t, true)
	edits := make([]contracts.HistoryEdit, 2*historywire.PruneBatch+5)
	for i := range edits {
		edits[i] = contracts.HistoryEdit{CallID: "c" + strconv.Itoa(i), Action: contracts.PruneCall}
	}
	type pruneRun struct {
		outcome contracts.PruneOutcome
		err     error
	}
	done := make(chan pruneRun, 1)
	go func() {
		outcome, err := bridgeHistory{driver: f.driver}.PruneHistory(context.Background(), edits)
		done <- pruneRun{outcome, err}
	}()
	chars := 9000
	for _, want := range []int{historywire.PruneBatch, historywire.PruneBatch, 5} {
		request := expectFrameOf(t, f, historywire.EventPrune)
		batch, err := historywire.DecodePrune(request)
		if err != nil || len(batch) != want {
			t.Fatalf("batch = %d edits, %v, want %d", len(batch), err, want)
		}
		answer, err := historywire.Pruned(contracts.PruneOutcome{Edited: len(batch), CharsBefore: chars, CharsAfter: chars - 1000})
		if err != nil {
			t.Fatalf("Pruned: %v", err)
		}
		chars -= 1000
		f.from <- historywire.Answer(request, answer)
	}
	run := <-done
	if run.err != nil || run.outcome != (contracts.PruneOutcome{Edited: len(edits), CharsBefore: 9000, CharsAfter: 6000}) {
		t.Fatalf("outcome = %+v, %v, want every edit counted from the first size to the last", run.outcome, run.err)
	}
}

func TestACallerDeadlineIsNotReportedAsAHistoryTimeout(t *testing.T) {
	f, _, _ := historyFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := bridgeHistory{driver: f.driver}.ReadHistory(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errHistoryTimeout) {
		t.Fatalf("ReadHistory = %v, want the caller's deadline and no history timeout", err)
	}
}
