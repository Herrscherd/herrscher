package host

import (
	"context"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type noticeRecord struct {
	conv   contracts.Conversation
	lang   i18n.Language
	notice contracts.Notice
}

type noticeGateway struct {
	fakeGateway

	mu      sync.Mutex
	records []noticeRecord
}

func (g *noticeGateway) Notify(_ context.Context, conv contracts.Conversation, lang i18n.Language, notice contracts.Notice) (contracts.MessageID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.records = append(g.records, noticeRecord{conv: conv, lang: lang, notice: notice})
	return "", nil
}

func (g *noticeGateway) received(t *testing.T, want int) []noticeRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		g.mu.Lock()
		records := append([]noticeRecord(nil), g.records...)
		g.mu.Unlock()
		if len(records) >= want {
			return records
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("gateway received no notice, want %d", want)
	return nil
}

func TestCompactionNoticeReachesTheGatewayTyped(t *testing.T) {
	gateway := &noticeGateway{}
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
	records := gateway.received(t, 1)
	got := records[0]
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

func TestCompactUnavailableNoticeNamesItsReason(t *testing.T) {
	f := newCompactFixture(t, "codex", compactPolicy(92))
	_, err := f.driver.compactStages()
	notice := contracts.NoticeOf(err)
	if notice.Code != contracts.CodeCompactUnavailable || len(notice.Args) != 1 {
		t.Fatalf("notice = %+v, want compact.unavailable with one argument", notice)
	}
	if notice.Args[0].Name() != "Reason" || notice.Args[0].Kind() != contracts.ArgText || notice.Args[0].Value() != errCompactUnsupported.Error() {
		t.Fatalf("argument = %s %d %v, want the Reason text", notice.Args[0].Name(), notice.Args[0].Kind(), notice.Args[0].Value())
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
