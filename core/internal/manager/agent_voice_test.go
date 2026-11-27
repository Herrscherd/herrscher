package manager

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

type fakeBotTokens map[string][]string

func (f fakeBotTokens) HasToken(gateway, name string) bool {
	return slices.Contains(f[gateway], name)
}

func createRin(t *testing.T, h *Handler) {
	t.Helper()
	if _, err := h.agentCreateRun(context.Background(), args("name", "rin", "soul", "persona")); err != nil {
		t.Fatalf("create rin: %v", err)
	}
}

func TestSetReplyTakesTheRuleAndChannelsAsOperands(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	createRin(t, h)
	if _, err := h.agentSetReplyRun(context.Background(), contracts.Input{Rest: []string{"rin", "channels", "chat:111", "other:-9"}}); err != nil {
		t.Fatalf("set-reply: %v", err)
	}
	reply, err := h.agents.Reply("rin")
	if err != nil || reply != contracts.ReplyInChannels {
		t.Fatalf("reply = %v, %v; want channels", reply, err)
	}
	channels, err := h.agents.Channels("rin")
	if err != nil || len(channels) != 2 {
		t.Fatalf("channels = %v, %v; want two", channels, err)
	}
}

func TestSetReplyRefusesUnknownRuleWithoutEchoingIt(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	createRin(t, h)
	_, err := h.agentSetReplyRun(context.Background(), contracts.Input{Rest: []string{"rin", "shouting"}})
	if !errors.Is(err, contracts.ErrUnknownReplyRule) {
		t.Fatalf("err = %v; want unknown reply rule", err)
	}
	if strings.Contains(err.Error(), "shouting") {
		t.Fatalf("the refused rule was echoed: %q", err)
	}
	reply, err := h.agents.Reply("rin")
	if err != nil || reply != contracts.ReplyOnMention {
		t.Fatalf("reply after a refused rule = %v, %v; want mention", reply, err)
	}
}

func TestSetReplyRefusesBadChannelAndKeepsTheRule(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	createRin(t, h)
	_, err := h.agentSetReplyRun(context.Background(), contracts.Input{Rest: []string{"rin", "never", "chat"}})
	if !errors.Is(err, agent.ErrBadChannel) {
		t.Fatalf("err = %v; want a bad channel", err)
	}
	reply, err := h.agents.Reply("rin")
	if err != nil || reply != contracts.ReplyOnMention {
		t.Fatalf("reply after a refused channel = %v, %v; want mention", reply, err)
	}
}

func TestAgentShowReportsVoiceAndTokenStateWithoutAnyToken(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	h.SetGatewayAdmins(map[string]contracts.ChannelAdmin{"chat": nil, "other": nil})
	h.SetBotTokens(fakeBotTokens{"chat": {"rin"}})
	createRin(t, h)
	if _, err := h.agentSetReplyRun(context.Background(), contracts.Input{Rest: []string{"rin", "channels", "chat:111"}}); err != nil {
		t.Fatalf("set-reply: %v", err)
	}
	out, err := cli.Plain(h.agentShowRun(context.Background(), args("name", "rin")))
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	for _, want := range []string{"reply: channels", "channels: chat:111", "bot token on chat: set", "bot token on other: not set"} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
}

func TestAgentShowJSONCarriesReplyChannelsAndTokenState(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	h.SetGatewayAdmins(map[string]contracts.ChannelAdmin{"chat": nil})
	h.SetBotTokens(fakeBotTokens{"chat": {"rin"}})
	createRin(t, h)
	if _, err := h.agentSetReplyRun(context.Background(), contracts.Input{Rest: []string{"rin", "channels", "chat:111"}}); err != nil {
		t.Fatalf("set-reply: %v", err)
	}
	out, err := cli.Plain(h.agentShowRun(context.Background(), contracts.Input{Args: map[string]string{"name": "rin"}, JSON: true}))
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	var got struct {
		Reply    string          `json:"reply"`
		Channels []string        `json:"channels"`
		Tokens   map[string]bool `json:"tokens"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if got.Reply != "channels" || len(got.Channels) != 1 || got.Channels[0] != "chat:111" || !got.Tokens["chat"] {
		t.Fatalf("show json = %+v", got)
	}
}
