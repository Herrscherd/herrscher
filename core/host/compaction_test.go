package host

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const quietWindow = 150 * time.Millisecond

type compactFixture struct {
	driver   *sessionDriver
	toBridge chan contracts.Event
	from     chan contracts.Event
	mu       sync.Mutex
	emitted  []contracts.Event
	seen     chan struct{}
}

func (f *compactFixture) tap(event contracts.Event) {
	f.mu.Lock()
	f.emitted = append(f.emitted, event)
	f.mu.Unlock()
	select {
	case f.seen <- struct{}{}:
	default:
	}
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
	return newConfiguredCompactFixture(t, vendor, policy, func(*sessionDriver) {})
}

func newConfiguredCompactFixture(t *testing.T, vendor string, policy compactionPolicy, configure func(*sessionDriver)) *compactFixture {
	t.Helper()
	t.Setenv(contextwindow.EnvVar(), "")
	f := &compactFixture{toBridge: make(chan contracts.Event, 16), from: make(chan contracts.Event, 16), seen: make(chan struct{}, 64)}
	f.driver = newSessionDriver("s1", nil, f.toBridge, f.from)
	f.driver.terms = func() modelTerms { return modelTerms{vendor: vendor} }
	f.driver.compaction = policy
	f.driver.emitTap = f.tap
	configure(f.driver)
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
		percent:        percent,
		names:          names,
		plugins:        make(map[string]contracts.Compactor),
		backends:       map[string]bool{"claude": true},
		historyEditors: make(map[string]bool),
		warnOnce:       &sync.Once{},
		historyOnce:    &sync.Once{},
		warn:           func(string, ...any) {},
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
	deadline := time.After(2 * time.Second)
	for {
		if events := f.emittedOf(kind); len(events) > 0 {
			return events[len(events)-1]
		}
		select {
		case <-f.seen:
		case <-deadline:
			t.Fatalf("no %s event was emitted", kind)
			return contracts.Event{}
		}
	}
}

func measuredAnswer(t *testing.T, after int) contracts.Event {
	t.Helper()
	return mustAnswer(t, contracts.CompactResult{TokensAfter: after, TokensMeasured: true, Note: "compacté"})
}

func unmeasuredAnswer(t *testing.T) contracts.Event {
	t.Helper()
	return mustAnswer(t, contracts.CompactResult{Note: "compacté"})
}

func mustAnswer(t *testing.T, result contracts.CompactResult) contracts.Event {
	t.Helper()
	frame, err := compactwire.Result(result)
	if err != nil {
		t.Fatalf("compactwire.Result: %v", err)
	}
	return frame
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
	f.from <- measuredAnswer(t, 4_000)
	if done := waitEmitted(t, f, eventCompactDone); done.Tokens != 4_000 {
		t.Fatalf("compaction_done = %+v, want 4000 measured", done)
	}
}

func TestAutomaticCompactionRunsOnceTheThresholdIsCrossed(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(50))
	openTurn(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, measuredAnswer(t, 20_000)) {
		t.Fatal("no automatic compaction after a turn at 75 percent of the window, threshold 50")
	}
	if done := waitEmitted(t, f, eventCompactDone); done.Value != contracts.TriggerAutomatic.String() || done.Tokens != 20_000 {
		t.Fatalf("compaction_done = %+v, want automatic down to 20000", done)
	}
}

func TestAutomaticCompactionDoesNotLoopOnAnUnmeasuredResult(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(50))
	openTurn(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, unmeasuredAnswer(t)) {
		t.Fatal("first automatic compaction did not run")
	}
	openTurn(t, f, 150_000)
	if compactFrameOrQuiet(t, f, measuredAnswer(t, 1)) {
		t.Fatal("compaction retriggered while usage stayed above the threshold: loop")
	}
	openTurn(t, f, 10_000)
	openTurn(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, measuredAnswer(t, 20_000)) {
		t.Fatal("compaction did not rearm after usage dropped under the threshold")
	}
}

func TestZeroPercentDisablesAutomaticCompaction(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(0))
	openTurn(t, f, 190_000)
	if compactFrameOrQuiet(t, f, measuredAnswer(t, 1)) {
		t.Fatal("automatic compaction ran with AutoCompactPercent 0")
	}
}

func TestBackendWithoutCompactorIsRefusedBeforeAnySend(t *testing.T) {
	gateway := newNoticeGateway()
	f := newConfiguredCompactFixture(t, "cursor", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.lang = i18n.French
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	failed := waitEmitted(t, f, eventCompactFailed)
	got := awaitNotice(t, gateway)
	if got.notice.Code != contracts.CodeCompactUnavailable {
		t.Fatalf("notice code = %v, want %v", got.notice.Code, contracts.CodeCompactUnavailable)
	}
	if failed.Text != "" {
		t.Fatalf("compaction_failed text = %q, want none while the linked gateway shows the notice", failed.Text)
	}
	expectNoFrame(t, f)
}

func TestCompactFailureTextIsKeptWhenNoGatewayIsLinked(t *testing.T) {
	f := newConfiguredCompactFixture(t, "cursor", compactPolicy(92), func(d *sessionDriver) {
		d.lang = i18n.French
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	failed := waitEmitted(t, f, eventCompactFailed)
	_, err := f.driver.compactStages(f.driver.compaction.names)
	requireRenderedText(t, failed.Text, contracts.NoticeOf(err), i18n.French)
	expectNoFrame(t, f)
}

func requireRenderedText(t *testing.T, text string, notice contracts.Notice, lang i18n.Language) {
	t.Helper()
	want, err := contracts.NewRenderer().Render(lang, notice)
	if err != nil {
		t.Fatalf("render %v in %s: %v", notice.Code, lang, err)
	}
	if text != want {
		t.Fatalf("compaction_failed text = %q, want the notice rendered in %s: %q", text, lang, want)
	}
}

func TestCodexWithCompactorIsCompacted(t *testing.T) {
	policy := compactPolicy(92)
	policy.backends["codex"] = true
	f := newCompactFixture(t, "codex", policy)
	f.driver.requestCompaction("", contracts.Conversation{})
	if got := nextFrame(t, f); got.T != compactwire.EventRequest {
		t.Fatalf("frame = %+v, want a compaction request", got)
	}
	f.from <- measuredAnswer(t, 30_000)
	if done := waitEmitted(t, f, eventCompactDone); done.Tokens != 30_000 {
		t.Fatalf("compaction_done = %+v, want 30000 measured", done)
	}
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
	f.from <- measuredAnswer(t, 30_000)
	if done := waitEmitted(t, f, eventCompactDone); done.Tokens != 30_000 {
		t.Fatalf("compaction_done = %+v, want the backend result last", done)
	}
}

func TestCompactionEventsCarryTokensOnlyWhenMeasured(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(92))
	f.driver.requestCompaction("", contracts.Conversation{})
	if got := nextFrame(t, f); got.T != compactwire.EventRequest {
		t.Fatalf("frame = %+v, want the compaction request", got)
	}
	if started := waitEmitted(t, f, eventCompactStarted); started.Done || started.Tokens != 0 || started.TokensIn != 0 {
		t.Fatalf("compaction_started = %+v, want no Done and no tokens without a measured turn", started)
	}
	f.from <- mustAnswer(t, contracts.CompactResult{TokensAfter: 50_000, Note: "compacté"})
	if done := waitEmitted(t, f, eventCompactDone); done.Done || done.Tokens != 0 || done.TokensIn != 0 {
		t.Fatalf("compaction_done = %+v, want no Done and no tokens for a size the stage did not measure", done)
	}
}

func TestMergedRequestIsNotAcceptedWhenTheFirstEnqueueFails(t *testing.T) {
	queue := make(chan queued, 1)
	queue <- queued{ev: contracts.Event{T: eventInput}}
	d := &sessionDriver{queue: queue, enqueueTimeout: 100 * time.Millisecond}
	first := make(chan [2]bool, 1)
	registered := make(chan struct{})
	d.compactRegistered = func() { close(registered) }
	go func() {
		merged, accepted := d.requestCompaction("premier", contracts.Conversation{})
		first <- [2]bool{merged, accepted}
	}()
	select {
	case <-registered:
	case <-time.After(2 * time.Second):
		t.Fatal("first request never registered")
	}
	merged, accepted := d.requestCompaction("second", contracts.Conversation{})
	if !merged || accepted {
		t.Fatalf("second request while the first waits = merged %t accepted %t, want merged and refused", merged, accepted)
	}
	if got := <-first; got[0] || got[1] {
		t.Fatalf("first request = merged %t accepted %t, want refused", got[0], got[1])
	}
	if n := len(queue); n != 1 {
		t.Fatalf("queue holds %d items, want only the filler", n)
	}
}

func TestMeasuredCompactionStillAboveThresholdDoesNotLoop(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(50))
	openTurn(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, measuredAnswer(t, 150_000)) {
		t.Fatal("first automatic compaction did not run")
	}
	openTurn(t, f, 150_000)
	if compactFrameOrQuiet(t, f, measuredAnswer(t, 120_000)) {
		t.Fatal("compaction retriggered after a measured result still above the threshold: loop")
	}
	openTurn(t, f, 10_000)
	openTurn(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, measuredAnswer(t, 20_000)) {
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

func TestCompactStackRefusesDuplicateStages(t *testing.T) {
	for _, names := range [][]string{{contracts.CompactStageBackend, contracts.CompactStageBackend}, {"stage", "stage"}} {
		if _, err := newCompactionPolicy(context.Background(), nil, names, nil); err == nil {
			t.Fatalf("newCompactionPolicy(%v) accepted a stage listed twice", names)
		}
	}
}

func TestInvalidContextLimitIsWarnedAboutOnce(t *testing.T) {
	warnings := 0
	warn := func(string, ...any) { warnings++ }
	warnInvalidContextLimit(warn, "many")
	if warnings != 1 {
		t.Fatalf("invalid HERRSCHER_CONTEXT_LIMIT wrote %d warnings, want 1", warnings)
	}
	warnInvalidContextLimit(warn, "")
	warnInvalidContextLimit(warn, "12345")
	if warnings != 1 {
		t.Fatalf("empty or valid HERRSCHER_CONTEXT_LIMIT wrote warnings: %d total", warnings)
	}
}

func TestCompactionPolicyTakesCodexFromItsManifest(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "codex", Category: contracts.CategoryBackend, Capabilities: contracts.Capabilities{Compactor: true}},
		Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) {
			return seedBackend{}, nil
		},
	})
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "cursor", Category: contracts.CategoryBackend},
		Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) {
			return seedBackend{}, nil
		},
	})
	policy, err := newCompactionPolicy(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("newCompactionPolicy: %v", err)
	}
	if !policy.supports("codex") {
		t.Fatal("codex declares Capabilities.Compactor in its manifest but the policy refuses it")
	}
	if policy.supports("cursor") {
		t.Fatal("cursor declares no Compactor but the policy accepts it")
	}
}

func TestContextLimitTableMovesTheAutomaticThreshold(t *testing.T) {
	t.Setenv(contextwindow.EnvVar(), "")
	policy := compactPolicy(50)
	policy.backends["codex"] = true
	policy.limits = contextwindow.NewLimits("", map[string]int{"codex": 400_000})
	f := newCompactFixture(t, "codex", policy)
	openTurn(t, f, 150_000)
	if compactFrameOrQuiet(t, f, measuredAnswer(t, 20_000)) {
		t.Fatal("automatic compaction ran at 37 percent of the 400000 window, threshold 50")
	}
}

func TestNewCompactionPolicyEnvWinsOverContextLimits(t *testing.T) {
	t.Setenv(contextwindow.EnvVar(), "")
	policy, err := newCompactionPolicy(context.Background(), nil, nil, map[string]int{"codex": 400_000})
	if err != nil {
		t.Fatalf("newCompactionPolicy: %v", err)
	}
	if got := policy.limits.Limit("codex"); got != 400_000 {
		t.Fatalf("Limit(codex) = %d, want the table's 400000", got)
	}
	t.Setenv(contextwindow.EnvVar(), "12345")
	policy, err = newCompactionPolicy(context.Background(), nil, nil, map[string]int{"codex": 400_000})
	if err != nil {
		t.Fatalf("newCompactionPolicy: %v", err)
	}
	if got := policy.limits.Limit("codex"); got != 12345 {
		t.Fatalf("Limit(codex) = %d, want the env's 12345", got)
	}
}

func TestUnknownContextLimitVendorIsWarnedAboutOnce(t *testing.T) {
	var warned []string
	warn := func(format string, args ...any) { warned = append(warned, fmt.Sprintf(format, args...)) }
	warnUnknownContextLimits(warn, map[string]int{"codex": 400_000, "Claude": 300_000, "ghost": 1}, []string{"codex", "claude"})
	if len(warned) != 1 || !strings.Contains(warned[0], `"ghost"`) {
		t.Fatalf("warnings = %q, want one line naming the unknown vendor ghost", warned)
	}
}

func TestSessionInfosCarryTheResolvedWindowPerVendor(t *testing.T) {
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if err = st.AddSession(state.Session{Name: "codexA", ChannelID: "c1", Vendor: "codex"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err = st.AddSession(state.Session{Name: "claudeA", ChannelID: "c2", Vendor: "claude"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	policy := compactPolicy(50)
	policy.limits = contextwindow.NewLimits("", map[string]int{"codex": 400_000})
	infos := sessionInfos(st, t.TempDir(), policy, modelTermsFor(nil))
	byName := make(map[string]contracts.SessionInfo, len(infos))
	for _, info := range infos {
		byName[info.Name] = info
	}
	if got := byName["codexA"].ContextWindow; got != 400_000 {
		t.Fatalf("codex session ContextWindow = %d, want the table's 400000", got)
	}
	if got := byName["claudeA"].ContextWindow; got != 200_000 {
		t.Fatalf("claude session ContextWindow = %d, want the built-in 200000", got)
	}
	if got := byName["codexA"].AutoCompactPercent; got != 50 {
		t.Fatalf("codex session AutoCompactPercent = %d, want 50", got)
	}
	raw, err := json.Marshal(infos)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"ContextWindow":400000`) {
		t.Fatalf("session info wire JSON lacks the window: %s", raw)
	}
}

func TestSessionInfosUseTheResolvedModelWindow(t *testing.T) {
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if err = st.AddSession(state.Session{Name: "claudeLong", ChannelID: "c3", Vendor: "claude", ModelID: "claude-long"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	terms := func(session state.Session) modelTerms {
		if session.ModelID == "claude-long" {
			return modelTerms{window: 1_000_000}
		}
		return modelTerms{}
	}
	infos := sessionInfos(st, t.TempDir(), compactPolicy(50), terms)
	if len(infos) != 1 || infos[0].ContextWindow != 1_000_000 {
		t.Fatalf("ContextWindow = %+v, want the model's 1000000", infos)
	}
}

func TestAutomaticCompactionStillAboveThresholdSaysSoInFrench(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(50))
	f.driver.lang = i18n.French
	openTurn(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, measuredAnswer(t, 150_000)) {
		t.Fatal("automatic compaction did not run")
	}
	status := waitEmitted(t, f, eventStatus)
	want := "compaction automatique sans effet : le contexte reste au-dessus de 50 %, pas de nouvelle tentative avant qu'il repasse sous le seuil"
	if status.Text != want {
		t.Fatalf("status = %q, want %q", status.Text, want)
	}
}
