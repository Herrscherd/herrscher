package host

import (
	"context"
	"io"
	"strconv"
	"testing"

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
