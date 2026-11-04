package host

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/contextwindow"
)

const quietWindow = 150 * time.Millisecond

type compactFixture struct {
	driver   *sessionDriver
	toBridge chan contracts.Event
	from     chan contracts.Event
	mu       sync.Mutex
	emitted  []contracts.Event
}

func (f *compactFixture) tap(event contracts.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emitted = append(f.emitted, event)
}

func (f *compactFixture) emittedOf(kind string) []contracts.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []contracts.Event
	for index := range f.emitted {
		if f.emitted[index].T == kind {
			out = append(out, f.emitted[index])
		}
	}
	return out
}

func newCompactFixture(t *testing.T, vendor string, policy compactionPolicy) *compactFixture {
	t.Helper()
	t.Setenv(contextwindow.EnvVar(), "")
	f := &compactFixture{toBridge: make(chan contracts.Event, 16), from: make(chan contracts.Event, 16)}
	f.driver = newSessionDriver("s1", nil, f.toBridge, f.from)
	f.driver.vendor = vendor
	f.driver.compaction = policy
	f.driver.emitTap = f.tap
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go f.driver.run(ctx)
	return f
}

func compactPolicy(percent int, names ...string) compactionPolicy {
	if len(names) == 0 {
		names = []string{contracts.CompactStageBackend}
	}
	return compactionPolicy{
		percent:  percent,
		names:    names,
		plugins:  make(map[string]contracts.Compactor),
		backends: map[string]bool{"claude": true},
		warnOnce: &sync.Once{},
		warn:     func(string, ...any) {},
	}
}

func nextFrame(t *testing.T, f *compactFixture) contracts.Event {
	t.Helper()
	select {
	case event := <-f.toBridge:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("no frame reached the bridge")
	}
	return contracts.Event{}
}

func expectNoFrame(t *testing.T, f *compactFixture) {
	t.Helper()
	select {
	case event := <-f.toBridge:
		t.Fatalf("frame %+v reached the bridge, want none", event)
	case <-time.After(quietWindow):
	}
}

func openTurn(t *testing.T, f *compactFixture, tokensIn int) {
	t.Helper()
	f.driver.queue <- queued{ev: contracts.Event{T: eventInput, Text: "vas-y"}}
	if in := nextFrame(t, f); in.T != eventInput {
		t.Fatalf("frame = %+v, want the input turn", in)
	}
	f.from <- contracts.Event{T: eventReply, Text: "fini", Done: true, TokensIn: tokensIn}
}

func compactFrameOrQuiet(t *testing.T, f *compactFixture, answer contracts.Event) bool {
	t.Helper()
	select {
	case event := <-f.toBridge:
		if event.T != compactwire.EventRequest {
			t.Fatalf("frame = %+v, want a compaction request", event)
		}
		f.from <- answer
		return true
	case <-time.After(quietWindow):
		return false
	}
}

func waitEmitted(t *testing.T, f *compactFixture, kind string) contracts.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if events := f.emittedOf(kind); len(events) > 0 {
			return events[len(events)-1]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s event was emitted", kind)
	return contracts.Event{}
}

func measuredAnswer(after int) contracts.Event {
	return compactwire.Result(contracts.CompactResult{TokensAfter: after, TokensMeasured: true, Note: "compacté"})
}

func unmeasuredAnswer() contracts.Event {
	return compactwire.Result(contracts.CompactResult{Note: "compacté"})
}

func TestCompactionNeverOverlapsATurn(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(92))
	f.driver.queue <- queued{ev: contracts.Event{T: eventInput, Text: "vas-y"}}
	if in := nextFrame(t, f); in.T != eventInput {
		t.Fatalf("frame = %+v, want the input turn", in)
	}
	f.driver.requestCompaction("", contracts.Conversation{})
	expectNoFrame(t, f)
	f.from <- contracts.Event{T: eventReply, Text: "fini", Done: true, TokensIn: 10}
	if got := nextFrame(t, f); got.T != compactwire.EventRequest || got.Value != contracts.TriggerManual.String() {
		t.Fatalf("frame after the turn = %+v, want the queued manual compaction", got)
	}
	f.from <- measuredAnswer(4_000)
	if done := waitEmitted(t, f, eventCompactDone); done.Tokens != 4_000 || !done.Done {
		t.Fatalf("compaction_done = %+v, want 4000 measured", done)
	}
}

func TestAutomaticCompactionRunsOnceTheThresholdIsCrossed(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(50))
	openTurn(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, measuredAnswer(20_000)) {
		t.Fatal("no automatic compaction after a turn at 75 percent of the window, threshold 50")
	}
	if done := waitEmitted(t, f, eventCompactDone); done.Value != contracts.TriggerAutomatic.String() || done.Tokens != 20_000 {
		t.Fatalf("compaction_done = %+v, want automatic down to 20000", done)
	}
}

func TestAutomaticCompactionDoesNotLoopOnAnUnmeasuredResult(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(50))
	openTurn(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, unmeasuredAnswer()) {
		t.Fatal("first automatic compaction did not run")
	}
	openTurn(t, f, 150_000)
	if compactFrameOrQuiet(t, f, measuredAnswer(1)) {
		t.Fatal("compaction retriggered while usage stayed above the threshold: loop")
	}
	openTurn(t, f, 10_000)
	openTurn(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, measuredAnswer(20_000)) {
		t.Fatal("compaction did not rearm after usage dropped under the threshold")
	}
}

func TestZeroPercentDisablesAutomaticCompaction(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(0))
	openTurn(t, f, 190_000)
	if compactFrameOrQuiet(t, f, measuredAnswer(1)) {
		t.Fatal("automatic compaction ran with AutoCompactPercent 0")
	}
}

func TestBackendWithoutCompactorIsRefusedBeforeAnySend(t *testing.T) {
	f := newCompactFixture(t, "codex", compactPolicy(92))
	f.driver.requestCompaction("", contracts.Conversation{})
	failed := waitEmitted(t, f, eventCompactFailed)
	if !strings.Contains(failed.Text, "does not implement Compactor") {
		t.Fatalf("compaction_failed = %+v, want the refusal", failed)
	}
	expectNoFrame(t, f)
}

func TestNoPortAnswerFailsTheCompaction(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(92))
	f.driver.requestCompaction("", contracts.Conversation{})
	if got := nextFrame(t, f); got.T != compactwire.EventRequest {
		t.Fatalf("frame = %+v, want a compaction request", got)
	}
	f.from <- compactwire.NoPort()
	waitEmitted(t, f, eventCompactFailed)
}

type recordingCompactor struct {
	name   string
	order  *[]string
	result contracts.CompactResult
}

func (c recordingCompactor) Compact(_ context.Context, _ contracts.CompactRequest, _ func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	*c.order = append(*c.order, c.name)
	return c.result, nil
}

func TestCustomStagesRunInConfiguredOrderBeforeTheBackend(t *testing.T) {
	var order []string
	policy := compactPolicy(92, "custom", contracts.CompactStageBackend)
	policy.plugins["custom"] = recordingCompactor{name: "custom", order: &order, result: contracts.CompactResult{TokensAfter: 50_000, TokensMeasured: true}}
	f := newCompactFixture(t, "claude", policy)
	f.driver.requestCompaction("garde les décisions", contracts.Conversation{})
	if got := nextFrame(t, f); got.T != compactwire.EventRequest || got.Text != "garde les décisions" {
		t.Fatalf("frame = %+v, want the backend stage with the instruction", got)
	}
	if strings.Join(order, ",") != "custom" {
		t.Fatalf("custom stage ran %v before the backend, want once", order)
	}
	f.from <- measuredAnswer(30_000)
	if done := waitEmitted(t, f, eventCompactDone); done.Tokens != 30_000 {
		t.Fatalf("compaction_done = %+v, want the backend result last", done)
	}
}

func TestMeasuredCompactionStillAboveThresholdDoesNotLoop(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(50))
	openTurn(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, measuredAnswer(150_000)) {
		t.Fatal("first automatic compaction did not run")
	}
	openTurn(t, f, 150_000)
	if compactFrameOrQuiet(t, f, measuredAnswer(120_000)) {
		t.Fatal("compaction retriggered after a measured result still above the threshold: loop")
	}
	openTurn(t, f, 10_000)
	openTurn(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, measuredAnswer(20_000)) {
		t.Fatal("compaction did not rearm after a measured turn under the threshold")
	}
}

func TestAbortedTurnDoesNotReuseThePreviousReading(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(50))
	var percent atomic.Int32
	f.driver.percentOf = func() int { return int(percent.Load()) }
	openTurn(t, f, 150_000)
	expectNoFrame(t, f)
	percent.Store(50)
	f.driver.queue <- queued{ev: contracts.Event{T: eventInput, Text: "tour coupé"}}
	if in := nextFrame(t, f); in.T != eventInput {
		t.Fatalf("frame = %+v, want the input turn", in)
	}
	close(f.driver.hangup)
	expectNoFrame(t, f)
	if started := f.emittedOf(eventCompactStarted); len(started) != 0 {
		t.Fatalf("an aborted turn started a compaction on the reading of the previous turn: %+v", started)
	}
}

func TestNoPortWarningIsWrittenOnce(t *testing.T) {
	policy := compactPolicy(92)
	var warned []string
	policy.warn = func(format string, _ ...any) { warned = append(warned, format) }
	policy.warnNoPort("claude")
	policy.warnNoPort("claude")
	if len(warned) != 1 {
		t.Fatalf("the no-port warning was written %d times, want once", len(warned))
	}
}
