package host

import (
	"context"
	"io"
	"strconv"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func TestABackendNoticeIsDeliveredToTheNoticeSink(t *testing.T) {
	gateway := newNoticeGateway()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "s1", []contracts.GatewaySet{{Gateway: gateway}}, nil, nil, io.Discard)
	go d.deliverNotices(ctx)
	guard := tokenGuard{}
	d.relayTurnEvent(ctx, &guard, contracts.Event{T: eventwire.EventBackendNotice, Value: strconv.FormatUint(uint64(contracts.CodeResumeLost), 10)}, 0)
	record := awaitNotice(t, gateway)
	if record.notice.Code != contracts.CodeResumeLost {
		t.Fatalf("delivered code = %v, want CodeResumeLost", record.notice.Code)
	}
}

func TestABackendNoticeReachesTheAttachedTabs(t *testing.T) {
	gateway := newNoticeGateway()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "s1", []contracts.GatewaySet{{Gateway: gateway}}, nil, nil, io.Discard)
	var tapped []contracts.Event
	d.emitTap = func(event contracts.Event) { tapped = append(tapped, event) }
	guard := tokenGuard{}
	d.relayTurnEvent(ctx, &guard, contracts.Event{T: eventwire.EventBackendNotice, Value: strconv.FormatUint(uint64(contracts.CodeResumeLost), 10)}, 0)
	want, err := contracts.NewRenderer().Render(d.textLanguage()(), contracts.Notice{Code: contracts.CodeResumeLost})
	if err != nil || want == "" {
		t.Fatalf("Render = %q, %v, want the resume notice text", want, err)
	}
	if len(tapped) != 1 || tapped[0].T != eventwire.EventNotice || tapped[0].Text != want {
		t.Fatalf("attached tabs got %+v, want one notice event with %q", tapped, want)
	}
	awaitNotice(t, gateway)
}

func TestABackendNoticeFailureRelaysNoCompactionFailure(t *testing.T) {
	gateway := &failingNoticeGateway{calls: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "s1", []contracts.GatewaySet{{Gateway: gateway}}, nil, nil, io.Discard)
	go d.deliverNotices(ctx)
	d.notifyBackendNotice(contracts.Event{T: eventwire.EventBackendNotice, Value: strconv.FormatUint(uint64(contracts.CodeResumeLost), 10)})
	awaitCall(t, gateway.calls)
	select {
	case failure := <-d.failures:
		t.Fatalf("backend notice relayed a compaction failure %+v", failure)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestABackendNoticeWithANonNumericCodeIsNotDelivered(t *testing.T) {
	for _, value := range []string{"resume_lost", "65536", "-1", ""} {
		gateway := newNoticeGateway()
		ctx, cancel := context.WithCancel(context.Background())
		d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "s1", []contracts.GatewaySet{{Gateway: gateway}}, nil, nil, io.Discard)
		go d.deliverNotices(ctx)
		d.notifyBackendNotice(contracts.Event{T: eventwire.EventBackendNotice, Value: value})
		expectNoNotice(t, gateway)
		cancel()
	}
}

type postRecorder struct {
	fakeGateway

	posts chan string
}

func (g *postRecorder) Post(_ context.Context, _ contracts.Conversation, text string) (contracts.MessageID, error) {
	g.posts <- text
	return "", nil
}

func TestABackendNoticeSkipsAGatewayWithoutNoticeSink(t *testing.T) {
	gateway := &postRecorder{posts: make(chan string, 8)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "s1", []contracts.GatewaySet{{Gateway: gateway}}, nil, nil, io.Discard)
	guard := tokenGuard{}
	d.relayTurnEvent(ctx, &guard, contracts.Event{T: eventwire.EventBackendNotice, Value: strconv.FormatUint(uint64(contracts.CodeResumeLost), 10)}, 0)
	if queued := len(d.notices); queued != 0 {
		t.Fatalf("%d notices queued for a gateway without NoticeSink, want none", queued)
	}
	select {
	case text := <-gateway.posts:
		t.Fatalf("gateway without NoticeSink received %q", text)
	case <-time.After(100 * time.Millisecond):
	}
}
