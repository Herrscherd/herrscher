package main

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/host"
)

// talker is a gateway that both contributes a verb and owns the main thread —
// the two capabilities bindGateways has to read before wrapping.
type talker struct{ ran chan struct{} }

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
		func(context.Context, contracts.Input) (string, error) { return "ok", nil },
	)}
}
func (t talker) RunForeground(context.Context, context.CancelFunc) error { return nil }

// The daemon only ever sees degraded gateways, so that is the shape the
// collection has to survive. Degrade's method set is fixed and carries no
// Commands, which is why bindGateways reads the verbs first; collecting them
// from what it returns yields nothing at all, and the whole plugin-command
// feature is dead in production while every unit test on raw fakes still
// passes. This test is the one that notices.
func TestContributedCommandsSurviveDegrading(t *testing.T) {
	sets := []host.Deps{{Gateway: talker{}}}

	gws, fg, extra, err := bindGateways(sets)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if fg == nil {
		t.Fatal("a gateway owning the main thread must be detected before wrapping")
	}

	// The premise: what the daemon runs on genuinely cannot be asked for verbs.
	if _, ok := gws[0].Gateway.(contracts.CommandSource); ok {
		t.Fatal("the wrapped gateway is expected to have lost Commands; this test proves nothing otherwise")
	}
	if again, _ := host.CollectCommands(gws); len(again) != 0 {
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
