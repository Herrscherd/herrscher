package host

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
)

func operatorReg(t *testing.T, ran *[]string) *cli.Registry {
	t.Helper()
	reg := &cli.Registry{}
	local := func(name string) func(context.Context, contracts.Input) (contracts.Result, error) {
		return func(context.Context, contracts.Input) (contracts.Result, error) {
			*ran = append(*ran, name)
			return contracts.DataResult("local " + name), nil
		}
	}
	for _, c := range []contracts.Cmd{
		contracts.New("session", "close").Param("name", "session name", true).
			Param("force", "kill it", false).Do(local("close")),
		contracts.New("session", "seed").Param("name", "session name", true).
			Param("task", "opening task", true).Do(local("seed")),
		contracts.New("host", "add").Param("name", "host name", true).Do(local("host add")),
		contracts.New("approve", "ask").Param("session", "session name", true).Do(local("approve ask")),
		contracts.New("approve", "list").Do(local("approve list")),
		contracts.New("memory", "record").Param("key", "node key", true).Do(local("record")),
	} {
		if err := reg.Add(c); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	return reg
}

func TestASessionVerbIsDecidedByTheRunningDaemon(t *testing.T) {
	var ran []string
	var sent []string
	reg := operatorReg(t, &ran)
	forwardDaemonOwnedCommands(reg, "inst", func(_ context.Context, path string, argv []string) (string, bool, error) {
		sent = argv
		if path == "" {
			t.Error("the forward got no socket path")
		}
		return "daemon closed it", true, nil
	})

	out, err := cli.Plain(reg.Dispatch(context.Background(), []string{"session", "close", "--name", "main", "--force"}))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out != "daemon closed it" {
		t.Fatalf("out = %q, want the daemon's own reply", out)
	}
	if len(ran) != 0 {
		t.Fatalf("ran locally: %v, want the daemon to decide", ran)
	}
	if got := strings.Join(sent, " "); got != "session close --name main --force" {
		t.Fatalf("argv = %q, want the invocation rebuilt as typed", got)
	}
}

func TestAHostVerbIsDecidedByTheRunningDaemon(t *testing.T) {
	var ran []string
	var sent []string
	reg := operatorReg(t, &ran)
	forwardDaemonOwnedCommands(reg, "inst", func(_ context.Context, _ string, argv []string) (string, bool, error) {
		sent = argv
		return "registered build1", true, nil
	})

	out, err := cli.Plain(reg.Dispatch(context.Background(), []string{"host", "add", "--name", "build1"}))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out != "registered build1" || len(ran) != 0 {
		t.Fatalf("out = %q ran = %v, want the daemon to decide", out, ran)
	}
	if got := strings.Join(sent, " "); got != "host add --name build1" {
		t.Fatalf("argv = %q, want the invocation rebuilt as typed", got)
	}
}

func TestWithNoDaemonListeningTheCommandStillRuns(t *testing.T) {
	var ran []string
	reg := operatorReg(t, &ran)
	forwardDaemonOwnedCommands(reg, "inst", func(context.Context, string, []string) (string, bool, error) {
		return "", false, nil
	})

	out, err := cli.Plain(reg.Dispatch(context.Background(), []string{"session", "close", "--name", "main"}))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out != "local close" || len(ran) != 1 {
		t.Fatalf("out = %q ran = %v, want the local fallback", out, ran)
	}
}

func TestApproveAskWithNoDaemonFailsFastInsteadOfWaiting(t *testing.T) {
	var ran []string
	reg := operatorReg(t, &ran)
	forwardDaemonOwnedCommands(reg, "inst", func(context.Context, string, []string) (string, bool, error) {
		return "", false, nil
	})

	_, err := reg.Dispatch(context.Background(), []string{"approve", "ask", "--session", "s1"})
	if err == nil {
		t.Fatal("want an error naming the daemon that is not there")
	}
	if !strings.Contains(err.Error(), "daemon") {
		t.Fatalf("err = %v, want the missing daemon named", err)
	}
	if len(ran) != 0 {
		t.Fatalf("ran locally: %v, want no request opened in a process that exits", ran)
	}

	if out, err := cli.Plain(reg.Dispatch(context.Background(), []string{"approve", "list"})); err != nil || out != "local approve list" {
		t.Fatalf("out = %q err = %v, want the other approve verbs to still fall back", out, err)
	}
}

func TestADaemonFailureIsNotRetriedLocally(t *testing.T) {
	var ran []string
	reg := operatorReg(t, &ran)
	forwardDaemonOwnedCommands(reg, "inst", func(context.Context, string, []string) (string, bool, error) {
		return "", true, errors.New("no session \"gone\"")
	})

	if _, err := reg.Dispatch(context.Background(), []string{"session", "close", "--name", "gone"}); err == nil {
		t.Fatal("want the daemon's error")
	}
	if len(ran) != 0 {
		t.Fatalf("ran locally: %v, want the daemon's refusal to stand", ran)
	}
}

func TestSeedAndOtherNamespacesAreLeftAlone(t *testing.T) {
	var ran []string
	reg := operatorReg(t, &ran)
	forwardDaemonOwnedCommands(reg, "inst", func(context.Context, string, []string) (string, bool, error) {
		t.Error("this command must not be forwarded here")
		return "", true, nil
	})

	for _, argv := range [][]string{
		{"session", "seed", "--name", "main", "--task", "go"},
		{"memory", "record", "--key", "k"},
	} {
		if _, err := reg.Dispatch(context.Background(), argv); err != nil {
			t.Fatalf("Dispatch %v: %v", argv, err)
		}
	}
	if len(ran) != 2 {
		t.Fatalf("ran = %v, want both run locally", ran)
	}
}

func TestTheWholeInvocationSurvivesTheTrip(t *testing.T) {
	var sent []string
	reg := &cli.Registry{}
	if err := reg.Add(contracts.New("session", "create").
		Param("name", "session name", true).
		ValueParam("model", "model id", false).
		Do(func(context.Context, contracts.Input) (contracts.Result, error) { return contracts.Result{}, nil })); err != nil {
		t.Fatalf("Add: %v", err)
	}
	forwardDaemonOwnedCommands(reg, "inst", func(_ context.Context, _ string, argv []string) (string, bool, error) {
		sent = argv
		return "ok", true, nil
	})

	if _, err := reg.Dispatch(context.Background(),
		[]string{"session", "create", "--json", "--name", "x", "--model", "opus", "extra"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	got := strings.Join(sent, " ")
	if !strings.Contains(got, "--model opus") || !strings.Contains(got, "extra") || !strings.HasSuffix(got, "--json") {
		t.Fatalf("argv = %q, want the value, the remainder and --json all carried", got)
	}
}
