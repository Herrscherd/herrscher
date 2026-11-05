package host

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/compactwire"
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
		d.lang = i18n.French
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
	f.from <- mustAnswer(t, contracts.CompactResult{
		TokensAfter:    50_000,
		TokensMeasured: true,
		Note:           "compacted",
		Notice:         contracts.Notice{Code: contracts.CodeCompacted},
	})
	if done := waitEmitted(t, f, eventCompactDone); done.Text != "" {
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
	if done := waitEmitted(t, f, eventCompactDone); done.Text != "compacté" {
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
	if done := waitEmitted(t, f, eventCompactDone); done.Text != "compacté" || done.Tokens != 50_000 {
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
		Value:  `{"code":1,"args":[{"name":"X","kind":9,"value":"a"}]}`,
	}
	if done := waitEmitted(t, f, eventCompactDone); done.Text != "compacté" || done.Tokens != 50_000 {
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
		d.lang = i18n.French
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
	f.from <- mustAnswer(t, contracts.CompactResult{
		TokensAfter:    50_000,
		TokensMeasured: true,
		Note:           "compacted",
		Notice:         contracts.Notice{Code: contracts.CodeCompacted},
	})
	waitEmitted(t, f, eventCompactDone)
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
}

func TestUnsupportedCompactionFailureTravelsAsANotice(t *testing.T) {
	gateway := newNoticeGateway()
	f := newConfiguredCompactFixture(t, "codex", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.lang = i18n.French
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	failed := waitEmitted(t, f, eventCompactFailed)
	got := awaitNotice(t, gateway)
	if got.notice.Code != contracts.CodeCompactUnavailable {
		t.Fatalf("notice = %+v, want compact.unavailable", got.notice)
	}
	if failed.Text != "" {
		t.Fatalf("compaction_failed text = %q, want none while the linked gateway shows the notice", failed.Text)
	}
	if reason := reasonText(t, got.notice); !strings.Contains(reason, `"s1"`) || !strings.Contains(reason, `"codex"`) {
		t.Fatalf("reason = %q, want the session and its vendor", reason)
	}
}

func TestCompactUnavailableNoticeNamesItsReason(t *testing.T) {
	f := newCompactFixture(t, "codex", compactPolicy(92))
	_, err := f.driver.compactStages()
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
	sessions := newSessionTable()
	h := newHub(
		state.NewState(filepath.Join(t.TempDir(), "state.json")),
		nil,
		nil,
		t.TempDir(),
		nil,
		nil,
		newApprovalTable(),
		sessions,
	)
	h.lang = i18n.French
	name := "lang-" + newTurnID()
	t.Cleanup(func() { h.goDead(name) })
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	h.goLive(ctx, state.Session{Name: name, Incarnation: "incarnation", Language: "en"})
	awaitRegisteredIncarnation(t, sessions, name, "incarnation")
	if got := sessions.lookup(name).lang; got != i18n.English {
		t.Fatalf("driver language = %v, want the session language English over the operator French", got)
	}
}

func openTurnFromElsewhere(t *testing.T, f *compactFixture, tokensIn int) {
	t.Helper()
	f.driver.queue <- queued{ev: contracts.Event{T: eventInput, Text: "vas-y"}, origin: contracts.Conversation{Gateway: "elsewhere"}}
	if in := nextFrame(t, f); in.T != eventInput {
		t.Fatalf("frame = %+v, want the input turn", in)
	}
	f.from <- contracts.Event{T: eventReply, Text: "fini", Done: true, TokensIn: tokensIn}
}

func TestAutomaticCompactionNoticeReachesTheGatewayAfterATurnFromElsewhere(t *testing.T) {
	gateway := newNoticeGateway()
	f := newConfiguredCompactFixture(t, "claude", compactPolicy(50), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.lang = i18n.French
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
		d.lang = i18n.French
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
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: gateway}}, nil, nil)
	parent, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.deliverNotices(parent)
	d.notifyGateways(contracts.Notice{Code: contracts.CodeCompacted})
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
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: gateway}}, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.deliverNotices(ctx)
	codes := []contracts.Code{contracts.CodeCompacted, contracts.CodeCompactUnavailable, contracts.Code(99)}
	for _, code := range codes {
		d.notifyGateways(contracts.Notice{Code: code})
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

func TestFailureTextIsKeptWhenTheFullQueueDropsItsNotice(t *testing.T) {
	gateway := newNoticeGateway()
	gateway.entered = make(chan struct{}, 1)
	gateway.release = make(chan struct{})
	t.Cleanup(func() { close(gateway.release) })
	f := newConfiguredCompactFixture(t, "cursor", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.lang = i18n.French
		d.noticeLog = slog.New(slog.NewTextHandler(&lockedBuffer{}, nil))
	})
	blocked := noticeDelivery{sink: gateway, conv: contracts.Conversation{}, lang: i18n.French, notice: contracts.Notice{Code: contracts.CodeCompacted}}
	for range noticeQueueCapacity + 1 {
		f.driver.notices <- blocked
	}
	select {
	case <-gateway.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("gateway never started the first notice")
	}
	f.driver.requestCompaction("", contracts.Conversation{})
	failed := waitEmitted(t, f, eventCompactFailed)
	_, err := f.driver.compactStages()
	requireRenderedText(t, failed.Text, contracts.NoticeOf(err), i18n.French)
}

func TestFullNoticeQueueDropsTheNoticeAndTheTurnAdvances(t *testing.T) {
	gateway := newNoticeGateway()
	gateway.entered = make(chan struct{}, 1)
	gateway.release = make(chan struct{})
	t.Cleanup(func() { close(gateway.release) })
	logs := &lockedBuffer{}
	f := newConfiguredCompactFixture(t, "claude", compactPolicy(92), func(d *sessionDriver) {
		d.gateways = []contracts.GatewaySet{{Gateway: gateway}}
		d.lang = i18n.French
		d.noticeLog = slog.New(slog.NewTextHandler(logs, nil))
	})
	blocked := noticeDelivery{sink: gateway, conv: contracts.Conversation{}, lang: i18n.French, notice: contracts.Notice{Code: contracts.CodeCompacted}}
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
	if done := waitEmitted(t, f, eventCompactDone); done.Tokens != 4_000 {
		t.Fatalf("compaction_done = %+v, want the turn to advance past the full queue", done)
	}
	if out := logs.String(); !strings.Contains(out, "dropped") || !strings.Contains(out, "session=s1") || !strings.Contains(out, "code=1") {
		t.Fatalf("log = %q, want the dropped notice with its session and code", out)
	}
}
