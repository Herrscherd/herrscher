package host

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestMCPWeightDoesNotWaitForTheDispatchLock(t *testing.T) {
	reg := cli.NewRegistry(clitext.NewMessages())
	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: io.Discard, reg: reg})
	if err := reg.Add(contracts.New(nsMCP, verbWeight).
		Do(func(context.Context, contracts.Input) (contracts.Result, error) {
			return contracts.DataResult("weighed"), nil
		})); err != nil {
		t.Fatal(err)
	}

	h.dispatchMu.Lock()
	defer h.dispatchMu.Unlock()
	done := make(chan struct{})
	var out string
	var err error
	go func() {
		out, err = cli.Plain(h.Dispatch(context.Background(), []string{nsMCP, verbWeight}))
		close(done)
	}()

	select {
	case <-done:
		if err != nil || out != "weighed" {
			t.Fatalf("Dispatch = (%q, %v), want weighed/nil", out, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("mcp weight waited for dispatchMu: a read-only report must not hold up the other commands")
	}
}

func TestMCPWeightRefusesWithoutADaemon(t *testing.T) {
	reg := cli.NewRegistry(clitext.NewMessages())
	if err := addVerbs(reg, mcpWeightVerb(clitext.NewMessages(), func() i18n.Language { return i18n.English }, nil)); err != nil {
		t.Fatalf("addVerbs: %v", err)
	}
	noDaemon := func(context.Context, string, []string) (string, bool, error) { return "", false, nil }
	forwardDaemonOwnedCommands(reg, RuntimeLayout{}, "inst", noDaemon)

	_, err := reg.Dispatch(context.Background(), []string{nsMCP, verbWeight, "--agent", "arc"})
	if !errors.Is(err, errDaemonRequired) {
		t.Fatalf("err = %v, want the daemon-required refusal", err)
	}
}

func TestMCPWeightIsAnsweredByTheDaemonWhenOneListens(t *testing.T) {
	reg := cli.NewRegistry(clitext.NewMessages())
	if err := addVerbs(reg, mcpWeightVerb(clitext.NewMessages(), func() i18n.Language { return i18n.English }, nil)); err != nil {
		t.Fatalf("addVerbs: %v", err)
	}
	var forwarded []string
	daemon := func(_ context.Context, _ string, argv []string) (string, bool, error) {
		forwarded = argv
		return "[]", true, nil
	}
	forwardDaemonOwnedCommands(reg, RuntimeLayout{}, "inst", daemon)

	out, err := cli.Plain(reg.Dispatch(context.Background(), []string{nsMCP, verbWeight, "--agent", "arc", "--deep"}))
	if err != nil || out != "[]" {
		t.Fatalf("Dispatch = (%q, %v), want the daemon's answer", out, err)
	}
	if len(forwarded) < 2 || forwarded[0] != nsMCP || forwarded[1] != verbWeight {
		t.Fatalf("forwarded argv = %v, want the mcp weight command", forwarded)
	}
}
