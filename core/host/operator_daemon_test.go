package host

import (
	"context"
	"errors"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestApproveAskRefusesWithoutADaemon(t *testing.T) {
	asked := false
	reg := cli.NewRegistry(clitext.NewMessages())
	ask := contracts.New(nsApprove, "ask").Do(func(context.Context, contracts.Input) (contracts.Result, error) {
		asked = true
		return contracts.Result{}, nil
	})
	if err := addVerbs(reg, ask); err != nil {
		t.Fatalf("addVerbs: %v", err)
	}
	noDaemon := func(context.Context, string, []string) (string, bool, error) { return "", false, nil }
	forwardDaemonOwnedCommands(reg, RuntimeLayout{}, "inst", noDaemon)

	_, err := reg.Dispatch(context.Background(), []string{nsApprove, "ask"})
	if !errors.Is(err, errDaemonRequired) {
		t.Fatalf("err = %v, want the daemon-required refusal", err)
	}
	if asked {
		t.Fatal("an approval was answered locally without a daemon: only the daemon holds the session tokens")
	}
}
