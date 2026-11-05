package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	discordgateway "github.com/Herrscherd/herrscher-discord-gateway"
)

type sentinelNoticeGateway struct {
	contracts.Gateway

	sends int
	err   error
}

func (g *sentinelNoticeGateway) Post(context.Context, contracts.Conversation, string) (contracts.MessageID, error) {
	g.sends++
	return "", nil
}

func (g *sentinelNoticeGateway) Notify(context.Context, contracts.Conversation, i18n.Language, contracts.Notice) (contracts.MessageID, error) {
	g.sends++
	return "", g.err
}

func TestDeliverNoticeSendsOnceWhenFallbackWasPosted(t *testing.T) {
	gateway := &sentinelNoticeGateway{err: fmt.Errorf("discord notify: %w: render failed", discordgateway.ErrFallbackPosted)}
	err := deliverNotice(context.Background(), gateway, contracts.Conversation{ID: "c"}, i18n.English, contracts.Notice{})
	if err != nil {
		t.Fatalf("deliverNotice returned %v, want nil once the fallback text is out", err)
	}
	if gateway.sends != 1 {
		t.Fatalf("the notice reached the channel %d times, want exactly 1", gateway.sends)
	}
}

func TestDeliverNoticeReportsOtherFailures(t *testing.T) {
	failure := errors.New("channel gone")
	gateway := &sentinelNoticeGateway{err: failure}
	err := deliverNotice(context.Background(), gateway, contracts.Conversation{ID: "c"}, i18n.English, contracts.Notice{})
	if !errors.Is(err, failure) {
		t.Fatalf("deliverNotice returned %v, want the original failure", err)
	}
	if gateway.sends != 1 {
		t.Fatalf("the notice was attempted %d times, want exactly 1", gateway.sends)
	}
}
