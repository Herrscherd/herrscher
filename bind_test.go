package main

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/host"
)

type talker struct{}

func (talker) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: "fake", Category: contracts.CategoryGateway}
}

func (talker) Post(context.Context, contracts.Conversation, string) (contracts.MessageID, error) {
	return "", nil
}

func (talker) Reply(context.Context, contracts.Conversation, contracts.MessageID, string) (contracts.MessageID, error) {
	return "", nil
}

func (talker) React(context.Context, contracts.Conversation, contracts.MessageID, string) error {
	return nil
}

func (talker) Menu(context.Context, contracts.Conversation, contracts.MessageID, string, []contracts.Choice) error {
	return nil
}

func (talker) Commands() []contracts.Cmd {
	return []contracts.Cmd{contracts.New("channel", "read").Help("fake").Do(
		func(context.Context, contracts.Input) (contracts.Result, error) {
			return contracts.DataResult("ok"), nil
		},
	)}
}
func (t talker) RunForeground(context.Context, context.CancelFunc) error { return nil }

func TestContributedCommandsSurviveDegrading(t *testing.T) {
	sets := []host.Deps{{Gateway: talker{}}}

	gws, _, fg, extra, err := bindGateways(sets)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if fg == nil {
		t.Fatal("a gateway owning the main thread must be detected before wrapping")
	}

	if _, ok := gws[0].Gateway.(contracts.CommandSource); ok {
		t.Fatal("the wrapped gateway is expected to have lost Commands; this test proves nothing otherwise")
	}
	again, err := host.CollectCommands(gws)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatal("collecting after wrapping is expected to yield nothing")
	}

	var got []string
	for _, c := range extra {
		got = append(got, strings.Join(c.Path, " "))
	}
	if len(got) != 1 || got[0] != "fake channel read" {
		t.Fatalf("the plugin's verb must reach the daemon namespaced under its kind, got %v", got)
	}
}
