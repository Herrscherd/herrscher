package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type noticeRecord struct {
	conv   contracts.Conversation
	lang   i18n.Language
	notice contracts.Notice
}

type noticeGateway struct {
	fakeGateway

	delivered chan noticeRecord
	entered   chan struct{}
	release   chan struct{}
}

func newNoticeGateway() *noticeGateway {
	return &noticeGateway{delivered: make(chan noticeRecord, 8)}
}

func (g *noticeGateway) Notify(ctx context.Context, conv contracts.Conversation, lang i18n.Language, notice contracts.Notice) (contracts.MessageID, error) {
	if g.entered != nil {
		g.entered <- struct{}{}
	}
	if g.release != nil {
		select {
		case <-g.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	g.delivered <- noticeRecord{conv: conv, lang: lang, notice: notice}
	return "", nil
}

func awaitNotice(t *testing.T, gateway *noticeGateway) noticeRecord {
	t.Helper()
	select {
	case record := <-gateway.delivered:
		return record
	case <-time.After(2 * time.Second):
		t.Fatal("gateway received no notice")
	}
	return noticeRecord{}
}

func expectNoNotice(t *testing.T, gateway *noticeGateway) {
	t.Helper()
	select {
	case record := <-gateway.delivered:
		t.Fatalf("gateway received notice %+v, want none", record.notice)
	case <-time.After(100 * time.Millisecond):
	}
}

func reasonText(t *testing.T, notice contracts.Notice) string {
	t.Helper()
	reason, ok := notice.Args[0].Value().(string)
	if !ok {
		t.Fatalf("reason argument = %v, want text", notice.Args[0].Value())
	}
	return reason
}

func TestCompactionNoticeReachesTheGatewayTyped(t *testing.T) {
	gateway := newNoticeGateway()
	f := newConfiguredCompactFixture(t, "claude", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.operator = fixedLanguageSource(i18n.French)
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
	f.from <- mustAnswer(t, contracts.CompactResult{
		TokensAfter:    50_000,
		TokensMeasured: true,
		Note:           "compacted",
		Notice:         contracts.Notice{Code: contracts.CodeCompacted},
	})
	if done := waitEmitted(t, f, eventwire.EventCompactionDone); done.Text != "" {
		t.Fatalf("compaction_done text = %q, want none once the notice carries the outcome", done.Text)
	}
	got := awaitNotice(t, gateway)
	if got.notice.Code != contracts.CodeCompacted || len(got.notice.Args) != 0 {
		t.Fatalf("notice = %+v, want compaction.done without arguments", got.notice)
	}
	if got.lang != i18n.French {
		t.Fatalf("notice language = %v, want the session language", got.lang)
	}
	renderer := contracts.NewRenderer()
	inFrench, err := renderer.Render(i18n.French, got.notice)
	if err != nil {
		t.Fatalf("Render fr: %v", err)
	}
	inEnglish, err := renderer.Render(i18n.English, got.notice)
	if err != nil {
		t.Fatalf("Render en: %v", err)
	}
	if inFrench == "" || inEnglish == "" || inFrench == inEnglish {
		t.Fatalf("rendered fr %q and en %q, want two different texts", inFrench, inEnglish)
	}
}

func TestCompactionWithoutNoticeKeepsTheNote(t *testing.T) {
	f := newCompactFixture(t, "claude", compactPolicy(92))
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
	f.from <- mustAnswer(t, contracts.CompactResult{TokensAfter: 50_000, TokensMeasured: true, Note: "compacté"})
	if done := waitEmitted(t, f, eventwire.EventCompactionDone); done.Text != "compacté" {
		t.Fatalf("compaction_done text = %q, want the legacy note", done.Text)
	}
}

func TestNoticeWithUnknownCodeFallsBackToTheNote(t *testing.T) {
	gateway := newNoticeGateway()
	f := newConfiguredCompactFixture(t, "claude", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
	f.from <- mustAnswer(t, contracts.CompactResult{
		TokensAfter:    50_000,
		TokensMeasured: true,
		Note:           "compacté",
		Notice:         contracts.Notice{Code: 99},
	})
	if done := waitEmitted(t, f, eventwire.EventCompactionDone); done.Text != "compacté" || done.Tokens != 50_000 {
		t.Fatalf("compaction_done = %+v, want the note and the measured figures", done)
	}
	expectNoNotice(t, gateway)
}

func TestMalformedNoticeKeepsTheNoteAndTheFigures(t *testing.T) {
	gateway := newNoticeGateway()
	f := newConfiguredCompactFixture(t, "claude", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
	f.from <- contracts.Event{
		T:      compactwire.EventResult,
		Text:   "compacté",
		Tokens: 50_000,
		Done:   true,
		Value:  `{"code":1,"args":[{"name":"X","kind":1}]}`,
	}
	if done := waitEmitted(t, f, eventwire.EventCompactionDone); done.Text != "compacté" || done.Tokens != 50_000 {
		t.Fatalf("compaction_done = %+v, want the note and the measured figures", done)
	}
	expectNoNotice(t, gateway)
}

func TestBlockedGatewayDoesNotStallTheTurns(t *testing.T) {
	gateway := newNoticeGateway()
	gateway.release = make(chan struct{})
	t.Cleanup(func() { close(gateway.release) })
	f := newConfiguredCompactFixture(t, "claude", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.operator = fixedLanguageSource(i18n.French)
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
	f.from <- mustAnswer(t, contracts.CompactResult{
		TokensAfter:    50_000,
		TokensMeasured: true,
		Note:           "compacted",
		Notice:         contracts.Notice{Code: contracts.CodeCompacted},
	})
	waitEmitted(t, f, eventwire.EventCompactionDone)
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
}

func TestUnsupportedCompactionFailureTravelsAsANotice(t *testing.T) {
	gateway := newNoticeGateway()
	f := newConfiguredCompactFixture(t, "codex", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.operator = fixedLanguageSource(i18n.French)
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	failed := waitEmitted(t, f, eventwire.EventCompactionFailed)
	got := awaitNotice(t, gateway)
	if got.notice.Code != contracts.CodeCompactUnavailable {
		t.Fatalf("notice = %+v, want compact.unavailable", got.notice)
	}
	requireFailure(t, failed, contracts.CodeCompactUnavailable, "")
	if reason := reasonText(t, got.notice); !strings.Contains(reason, `"s1"`) || !strings.Contains(reason, `"codex"`) {
		t.Fatalf("reason = %q, want the session and its vendor", reason)
	}
}

func TestCompactUnavailableNoticeNamesItsReason(t *testing.T) {
	f := newCompactFixture(t, "codex", compactPolicy(92))
	_, err := f.driver.compactStages(f.driver.compaction.names, stageNotes{})
	notice := contracts.NoticeOf(err)
	if notice.Code != contracts.CodeCompactUnavailable || len(notice.Args) != 1 {
		t.Fatalf("notice = %+v, want compact.unavailable with one argument", notice)
	}
	if notice.Args[0].Name() != "Reason" || notice.Args[0].Kind() != contracts.ArgText {
		t.Fatalf("argument = %s %d, want the Reason text", notice.Args[0].Name(), notice.Args[0].Kind())
	}
	if reason := reasonText(t, notice); !strings.Contains(reason, `"s1"`) || !strings.Contains(reason, `"codex"`) {
		t.Fatalf("reason = %q, want the session and its vendor", reason)
	}
}

func TestQueueFullNoticeNamesTheSession(t *testing.T) {
	notice := contracts.NoticeOf(queueFullCompaction("s1"))
	if notice.Code != contracts.CodeCompactUnavailable {
		t.Fatalf("notice = %+v, want compact.unavailable", notice)
	}
	if reason := reasonText(t, notice); !strings.Contains(reason, `"s1"`) {
		t.Fatalf("reason = %q, want the session", reason)
	}
}

func TestSessionLanguageFallsBackToTheOperator(t *testing.T) {
	if got := sessionLanguage(state.Session{Language: "fr"}, i18n.English); got != i18n.French {
		t.Fatalf("sessionLanguage(fr) = %v, want French", got)
	}
	if got := sessionLanguage(state.Session{}, i18n.French); got != i18n.French {
		t.Fatalf("sessionLanguage(empty) = %v, want the operator language", got)
	}
}

func TestGoLiveUsesTheSessionLanguage(t *testing.T) {
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	store := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	name := "lang-" + newTurnID()
	if err := store.AddSession(state.Session{Name: name, Incarnation: "incarnation", Language: "en"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	h := newHub(
		store,
		nil,
		nil,
		t.TempDir(),
		nil,
		nil,
		newApprovalTable(io.Discard),
		sessions,
	)
	h.runtime = isolatedRuntimeDir(t)
	h.lang = i18n.French
	t.Cleanup(func() { h.goDead(name) })
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	if err := h.goLive(ctx, state.Session{Name: name, Incarnation: "incarnation", Language: "en"}); err != nil {
		t.Fatal(err)
	}
	awaitRegisteredIncarnation(t, sessions, name, "incarnation")
	if got := sessions.lookup(name).textLanguage()(); got != i18n.English {
		t.Fatalf("driver language = %v, want the session language English over the operator French", got)
	}
}

func openTurnFromElsewhere(t *testing.T, f *compactFixture, tokensIn int) {
	t.Helper()
	f.driver.queue <- queued{ev: contracts.Event{T: eventwire.EventInput, Text: "vas-y"}, origin: contracts.Conversation{Gateway: "elsewhere"}}
	if in := nextFrame(t, f); in.T != eventwire.EventInput {
		t.Fatalf("frame = %+v, want the input turn", in)
	}
	f.from <- contracts.Event{T: eventwire.EventReply, Text: "fini", Done: true, TokensIn: tokensIn}
}

func TestAutomaticCompactionNoticeReachesTheGatewayAfterATurnFromElsewhere(t *testing.T) {
	gateway := newNoticeGateway()
	f := newConfiguredCompactFixture(t, "claude", compactPolicy(50), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.operator = fixedLanguageSource(i18n.French)
	})
	openTurnFromElsewhere(t, f, 150_000)
	if !compactFrameOrQuiet(t, f, mustAnswer(t, contracts.CompactResult{TokensAfter: 20_000, TokensMeasured: true, Note: "compacté", Notice: contracts.Notice{Code: contracts.CodeCompacted}})) {
		t.Fatal("no automatic compaction after a turn from elsewhere")
	}
	if got := awaitNotice(t, gateway); got.notice.Code != contracts.CodeCompacted {
		t.Fatalf("notice = %+v, want compaction.done", got.notice)
	}
}

func TestManualCompactionNoticeReachesTheGatewayAfterATurnFromElsewhere(t *testing.T) {
	gateway := newNoticeGateway()
	f := newConfiguredCompactFixture(t, "claude", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.operator = fixedLanguageSource(i18n.French)
	})
	openTurnFromElsewhere(t, f, 10)
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
	f.from <- mustAnswer(t, contracts.CompactResult{TokensAfter: 4_000, TokensMeasured: true, Note: "compacté", Notice: contracts.Notice{Code: contracts.CodeCompacted}})
	if got := awaitNotice(t, gateway); got.notice.Code != contracts.CodeCompacted {
		t.Fatalf("notice = %+v, want compaction.done", got.notice)
	}
}

func TestNoticeIsDeliveredWhenTheParentContextIsCancelledDuringANotify(t *testing.T) {
	gateway := newNoticeGateway()
	gateway.entered = make(chan struct{}, 1)
	gateway.release = make(chan struct{})
	d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "s1", []contracts.GatewaySet{{Gateway: gateway}}, nil, nil, io.Discard)
	parent, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.deliverNotices(parent)
	d.notifyGateways(contracts.Notice{Code: contracts.CodeCompacted}, nil)
	select {
	case <-gateway.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("gateway never started the notice")
	}
	cancel()
	close(gateway.release)
	if got := awaitNotice(t, gateway); got.notice.Code != contracts.CodeCompacted {
		t.Fatalf("notice = %+v, want compaction.done after the parent was cancelled", got.notice)
	}
}

func TestNoticesReachTheGatewayInTheOrderTheyWereRaised(t *testing.T) {
	gateway := newNoticeGateway()
	d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "s1", []contracts.GatewaySet{{Gateway: gateway}}, nil, nil, io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.deliverNotices(ctx)
	codes := []contracts.Code{contracts.CodeCompacted, contracts.CodeCompactUnavailable, contracts.Code(99)}
	for _, code := range codes {
		d.notifyGateways(contracts.Notice{Code: code}, nil)
	}
	for _, code := range codes {
		if got := awaitNotice(t, gateway); got.notice.Code != code {
			t.Fatalf("notice = %d, want %d in raising order", got.notice.Code, code)
		}
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestFailurePayloadIsKeptWhenTheFullQueueDropsItsNotice(t *testing.T) {
	gateway := newNoticeGateway()
	gateway.entered = make(chan struct{}, 1)
	gateway.release = make(chan struct{})
	t.Cleanup(func() { close(gateway.release) })
	f := newConfiguredCompactFixture(t, "cursor", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.operator = fixedLanguageSource(i18n.French)
		d.noticeLog = slog.New(slog.NewTextHandler(&lockedBuffer{}, nil))
	})
	blocked := noticeDelivery{sink: gateway, conv: contracts.Conversation{}, lang: fixedLanguageSource(i18n.French), notice: contracts.Notice{Code: contracts.CodeCompacted}}
	for range noticeQueueCapacity + 1 {
		f.driver.notices <- blocked
	}
	select {
	case <-gateway.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("gateway never started the first notice")
	}
	f.driver.requestCompaction("", contracts.Conversation{})
	failed := waitEmitted(t, f, eventwire.EventCompactionFailed)
	_, err := f.driver.compactStages(f.driver.compaction.names, stageNotes{})
	requireFailure(t, failed, contracts.NoticeOf(err).Code, "")
}

func TestFullNoticeQueueDropsTheNoticeAndTheTurnAdvances(t *testing.T) {
	gateway := newNoticeGateway()
	gateway.entered = make(chan struct{}, 1)
	gateway.release = make(chan struct{})
	t.Cleanup(func() { close(gateway.release) })
	logs := &lockedBuffer{}
	f := newConfiguredCompactFixture(t, "claude", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.operator = fixedLanguageSource(i18n.French)
		d.noticeLog = slog.New(slog.NewTextHandler(logs, nil))
	})
	blocked := noticeDelivery{sink: gateway, conv: contracts.Conversation{}, lang: fixedLanguageSource(i18n.French), notice: contracts.Notice{Code: contracts.CodeCompacted}}
	for range noticeQueueCapacity + 1 {
		f.driver.notices <- blocked
	}
	select {
	case <-gateway.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("gateway never started the first notice")
	}
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
	f.from <- mustAnswer(t, contracts.CompactResult{TokensAfter: 4_000, TokensMeasured: true, Note: "compacté", Notice: contracts.Notice{Code: contracts.CodeCompacted}})
	if done := waitEmitted(t, f, eventwire.EventCompactionDone); done.Tokens != 4_000 {
		t.Fatalf("compaction_done = %+v, want the turn to advance past the full queue", done)
	}
	if out := logs.String(); !strings.Contains(out, "dropped") || !strings.Contains(out, "session=s1") || !strings.Contains(out, "code=1") {
		t.Fatalf("log = %q, want the dropped notice with its session and code", out)
	}
}

type failingNoticeGateway struct {
	fakeGateway

	calls  chan struct{}
	events chan contracts.Event
}

func newFailingNoticeGateway() *failingNoticeGateway {
	return &failingNoticeGateway{calls: make(chan struct{}, 1), events: make(chan contracts.Event, 8)}
}

func (g *failingNoticeGateway) Notify(context.Context, contracts.Conversation, i18n.Language, contracts.Notice) (contracts.MessageID, error) {
	g.calls <- struct{}{}
	return "", errors.New("gateway down")
}

func (g *failingNoticeGateway) Emit(event contracts.Event) {
	g.events <- event
}

func awaitCall(t *testing.T, calls chan struct{}) {
	t.Helper()
	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("gateway was never asked to deliver the notice")
	}
}

func awaitRelayedText(t *testing.T, events chan contracts.Event) contracts.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Text != "" {
				return event
			}
		case <-deadline:
			t.Fatal("gateway received no event carrying a text")
		}
	}
}

func TestNoticeDeliveryFailureRelaysTheSuccessAsDoneNeverAsAFailure(t *testing.T) {
	first := newFailingNoticeGateway()
	second := newFailingNoticeGateway()
	f := newConfiguredCompactFixture(t, "claude", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: first}, {Gateway: second}}
		d.operator = fixedLanguageSource(i18n.French)
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
	f.from <- mustAnswer(t, contracts.CompactResult{TokensAfter: 50_000, TokensMeasured: true, Note: "compacted", Notice: contracts.Notice{Code: contracts.CodeCompacted}})
	waitEmitted(t, f, eventwire.EventCompactionDone)
	want, err := contracts.NewRenderer().Render(i18n.French, contracts.Notice{Code: contracts.CodeCompacted})
	if err != nil {
		t.Fatalf("Render fr: %v", err)
	}
	for _, gateway := range []*failingNoticeGateway{first, second} {
		awaitCall(t, gateway.calls)
		relayed := awaitRelayedText(t, gateway.events)
		if relayed.T != eventwire.EventCompactionDone || relayed.Text != want || relayed.Tokens != 50_000 {
			t.Fatalf("relayed = %+v, want compaction_done with the French notice and the measured figures", relayed)
		}
	}
	<-time.After(100 * time.Millisecond)
	if n := len(f.emittedOf(eventwire.EventCompactionFailed)); n != 0 {
		t.Fatalf("compaction_failed tapped %d times, want none for a success", n)
	}
}

type postOnlyGateway struct {
	fakeGateway

	posted chan string
}

func (g *postOnlyGateway) Post(_ context.Context, _ contracts.Conversation, text string) (contracts.MessageID, error) {
	g.posted <- text
	return "", nil
}

func awaitPosted(t *testing.T, gateway *postOnlyGateway) string {
	t.Helper()
	select {
	case text := <-gateway.posted:
		return text
	case <-time.After(2 * time.Second):
		t.Fatal("post-only gateway received no text")
	}
	return ""
}

func TestPostOnlyGatewayAndNoticeSinkEachReceiveTheNoticeOnce(t *testing.T) {
	postOnly := &postOnlyGateway{posted: make(chan string, 4)}
	sink := newNoticeGateway()
	f := newConfiguredCompactFixture(t, "cursor", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: contracts.Degrade(postOnly)}, {Gateway: sink}}
		d.operator = fixedLanguageSource(i18n.French)
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	failed := waitEmitted(t, f, eventwire.EventCompactionFailed)
	requireFailure(t, failed, contracts.CodeCompactUnavailable, "")
	got := awaitNotice(t, sink)
	if got.notice.Code != contracts.CodeCompactUnavailable {
		t.Fatalf("notice = %+v, want compact.unavailable", got.notice)
	}
	want, err := contracts.NewRenderer().Render(i18n.French, got.notice)
	if err != nil {
		t.Fatalf("Render fr: %v", err)
	}
	if text := awaitPosted(t, postOnly); text != want {
		t.Fatalf("post-only gateway text = %q, want %q", text, want)
	}
	select {
	case text := <-postOnly.posted:
		t.Fatalf("post-only gateway received a second text %q, want one", text)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCompactionFailureReasonReachesTheTapWithoutAGateway(t *testing.T) {
	d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "s1", nil, nil, nil, io.Discard)
	var tapped []contracts.Event
	d.emitTap = func(event contracts.Event) { tapped = append(tapped, event) }
	d.operator = fixedLanguageSource(i18n.French)

	d.failCompaction(context.Background(), contracts.TriggerManual, errBridgeGone)

	if len(tapped) != 1 || tapped[0].T != eventwire.EventCompactionFailed {
		t.Fatalf("tapped = %+v, want one compaction_failed", tapped)
	}
	requireFailure(t, tapped[0], 0, eventwire.CompactReasonBridgeGone)
}

func TestRefusedFailureNoticeTapsTheFailureOnce(t *testing.T) {
	gateway := newFailingNoticeGateway()
	f := newConfiguredCompactFixture(t, "cursor", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.operator = fixedLanguageSource(i18n.French)
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	failed := waitEmitted(t, f, eventwire.EventCompactionFailed)
	awaitCall(t, gateway.calls)
	requireFailure(t, failed, contracts.CodeCompactUnavailable, "")
	<-time.After(100 * time.Millisecond)
	if n := len(f.emittedOf(eventwire.EventCompactionFailed)); n != 1 {
		t.Fatalf("compaction_failed tapped %d times, want exactly once", n)
	}
}

type eventCounter struct {
	fakeGateway

	emitted chan contracts.Event
}

func (g *eventCounter) Emit(event contracts.Event) {
	g.emitted <- event
}

type noticeCounter struct {
	eventCounter

	refuse   bool
	notified chan contracts.Notice
}

func (g *noticeCounter) Notify(_ context.Context, _ contracts.Conversation, _ i18n.Language, notice contracts.Notice) (contracts.MessageID, error) {
	g.notified <- notice
	if g.refuse {
		return "", errors.New("gateway down")
	}
	return "", nil
}

func drainEmitted(events chan contracts.Event) []contracts.Event {
	var out []contracts.Event
	idle := time.After(300 * time.Millisecond)
	for {
		select {
		case event := <-events:
			out = append(out, event)
			idle = time.After(300 * time.Millisecond)
		case <-idle:
			return out
		}
	}
}

func countFailureLines(t *testing.T, events []contracts.Event) int {
	t.Helper()
	lines := 0
	for _, event := range events {
		payload, err := eventwire.DecodeCompactionFailure(event.Value)
		if err != nil {
			t.Fatalf("compaction_failed value = %q, want a payload: %v", event.Value, err)
		}
		if payload.Code != 0 || payload.Reason != "" {
			lines++
		}
	}
	return lines
}

func TestCompactionFailureShowsOneLinePerGatewayAndTapsOnce(t *testing.T) {
	cases := []struct {
		name       string
		sink       bool
		refuse     bool
		full       bool
		wantEmits  int
		wantLines  int
		wantNotify bool
	}{
		{name: "accepted by the sink", sink: true, wantEmits: 1, wantLines: 0, wantNotify: true},
		{name: "refused by the sink", sink: true, refuse: true, wantEmits: 2, wantLines: 1, wantNotify: true},
		{name: "full queue", sink: true, full: true, wantEmits: 1, wantLines: 1},
		{name: "no sink", wantEmits: 1, wantLines: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emitted := make(chan contracts.Event, 8)
			notified := make(chan contracts.Notice, 8)
			var gateway contracts.GatewaySet
			if tc.sink {
				gateway = contracts.GatewaySet{Gateway: &noticeCounter{eventCounter: eventCounter{emitted: emitted}, refuse: tc.refuse, notified: notified}}
			} else {
				gateway = contracts.GatewaySet{Gateway: &eventCounter{emitted: emitted}}
			}
			d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "s1", []contracts.GatewaySet{gateway}, nil, nil, io.Discard)
			d.operator = fixedLanguageSource(i18n.French)
			var tapped []contracts.Event
			d.emitTap = func(event contracts.Event) { tapped = append(tapped, event) }
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			if tc.full {
				d.notices = make(chan noticeDelivery)
			} else {
				go d.deliverNotices(ctx)
			}
			go d.pump(ctx)

			d.failCompaction(ctx, contracts.TriggerManual, queueFullCompaction("s1"))

			if len(tapped) != 1 || tapped[0].T != eventwire.EventCompactionFailed {
				t.Fatalf("tapped = %+v, want one compaction_failed", tapped)
			}
			requireFailure(t, tapped[0], contracts.CodeCompactUnavailable, "")
			got := drainEmitted(emitted)
			if len(got) != tc.wantEmits {
				t.Fatalf("gateway received %d events, want %d: %+v", len(got), tc.wantEmits, got)
			}
			if lines := countFailureLines(t, got); lines != tc.wantLines {
				t.Fatalf("gateway shows %d failure lines, want %d", lines, tc.wantLines)
			}
			if tc.wantNotify != (len(notified) == 1) {
				t.Fatalf("notice delivered = %t, want %t", len(notified) == 1, tc.wantNotify)
			}
		})
	}
}
