package host

import (
	"context"
	"errors"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type kindedNoticeGateway struct {
	*noticeGateway

	kind string
}

func (g kindedNoticeGateway) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: g.kind, Category: contracts.CategoryGateway}
}

type bannerGateway struct {
	fakeGateway

	posted []contracts.Conversation
}

func (g *bannerGateway) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: "telegram", Category: contracts.CategoryGateway}
}

func (g *bannerGateway) Post(_ context.Context, conv contracts.Conversation, _ string) (contracts.MessageID, error) {
	g.posted = append(g.posted, conv)
	return "1", nil
}

func TestAGatewayWithoutChannelAdminStillReceivesItsSessionBanner(t *testing.T) {
	telegram := &bannerGateway{}
	home := labeledAdmin{"chat"}
	admins := gatewayAdmins([]Deps{{Gateway: kindedNoticeGateway{newNoticeGateway(), "chat"}, Admin: home}, {Gateway: telegram}})

	if admins["chat"] != home {
		t.Fatalf("the home gateway lost its own admin: %T", admins["chat"])
	}
	own := admins["telegram"]
	if err := own.Send(t.Context(), "42", "banner"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(telegram.posted) != 1 || telegram.posted[0] != (contracts.Conversation{Gateway: "telegram", ID: "42"}) {
		t.Fatalf("posted %+v", telegram.posted)
	}
	if own.ChannelRef("42") != "42" {
		t.Fatalf("ref = %q", own.ChannelRef("42"))
	}
	if _, err := own.CreateUnder(t.Context(), "home", "x"); !errors.Is(err, errGatewayHasNoChannels) {
		t.Fatalf("create under = %v", err)
	}
}

func TestAnAccountNoticeReachesTheGatewayTheSessionAdopted(t *testing.T) {
	home := newNoticeGateway()
	telegram := newNoticeGateway()
	fallback := newAccountFallback(nil, []Deps{
		{Gateway: kindedNoticeGateway{home, "chat"}, Admin: labeledAdmin{"chat"}},
		{Gateway: kindedNoticeGateway{telegram, "telegram"}},
	})
	notice := contracts.Notice{Code: contracts.CodeAccountFallback, Args: []contracts.Arg{contracts.Text("From", "work"), contracts.Text("To", "perso")}}

	fallback.Notify(t.Context(), state.HomeRef{ID: "home", Type: "forum"}, []string{"telegram"}, "42", i18n.English, notice)

	record := awaitNotice(t, telegram)
	if record.conv != (contracts.Conversation{Gateway: "telegram", ID: "42"}) {
		t.Fatalf("delivered to %+v", record.conv)
	}
	if len(home.delivered) != 0 {
		t.Fatal("the home gateway also got the notice")
	}
}

func TestAnAccountNoticeForAGatewayThatIsNotRunningIsDropped(t *testing.T) {
	home := newNoticeGateway()
	fallback := newAccountFallback(nil, []Deps{{Gateway: kindedNoticeGateway{home, "chat"}, Admin: labeledAdmin{"chat"}}})
	notice := contracts.Notice{Code: contracts.CodeAccountFallback, Args: []contracts.Arg{contracts.Text("From", "work"), contracts.Text("To", "perso")}}

	fallback.Notify(t.Context(), state.HomeRef{ID: "home", Type: "forum"}, []string{"telegram"}, "43", i18n.English, notice)

	if len(home.delivered) != 0 {
		t.Fatal("the home gateway got the notice of a conversation it does not own")
	}
}
