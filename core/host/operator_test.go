package host

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
)

// operatorReg is a registry shaped like the operator CLI's: a couple of session
// verbs whose handlers record that they ran here rather than in the daemon.
func operatorReg(t *testing.T, ran *[]string) *cli.Registry {
	t.Helper()
	reg := &cli.Registry{}
	local := func(name string) func(context.Context, contracts.Input) (string, error) {
		return func(context.Context, contracts.Input) (string, error) {
			*ran = append(*ran, name)
			return "local " + name, nil
		}
	}
	for _, c := range []contracts.Cmd{
		contracts.New("session", "close").Param("name", "session name", true).
			Param("force", "kill it", false).Do(local("close")),
		contracts.New("session", "seed").Param("name", "session name", true).
			Param("task", "opening task", true).Do(local("seed")),
		contracts.New("host", "add").Param("name", "host name", true).Do(local("host add")),
		contracts.New("memory", "record").Param("key", "node key", true).Do(local("record")),
	} {
		if err := reg.Add(c); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	return reg
}

// The bug this fixes: `session close` from the CLI mutated that process's copy
// of the state, so the running daemon kept the session — still offering it for
// completion, and writing it back on its next save.
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

	out, err := reg.Dispatch(context.Background(), []string{"session", "close", "--name", "main", "--force"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out != "daemon closed it" {
		t.Fatalf("out = %q, want the daemon's own reply", out)
	}
	if len(ran) != 0 {
		t.Fatalf("ran locally: %v, want the daemon to decide", ran)
	}
	// A valueless boolean goes back valueless, or "true" lands as a positional.
	if got := strings.Join(sent, " "); got != "session close --name main --force" {
		t.Fatalf("argv = %q, want the invocation rebuilt as typed", got)
	}
}

// Host records live in the same file as the sessions, so they have the same
// problem: a host added here would be dropped by the daemon's next save, and the
// session that named it would refuse for want of a machine just registered.
func TestAHostVerbIsDecidedByTheRunningDaemon(t *testing.T) {
	var ran []string
	var sent []string
	reg := operatorReg(t, &ran)
	forwardDaemonOwnedCommands(reg, "inst", func(_ context.Context, _ string, argv []string) (string, bool, error) {
		sent = argv
		return "registered build1", true, nil
	})

	out, err := reg.Dispatch(context.Background(), []string{"host", "add", "--name", "build1"})
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

// No daemon is a normal state — a one-shot install, or the CLI used before
// `service install`. The dial misses and the command runs here, as it always did.
func TestWithNoDaemonListeningTheCommandStillRuns(t *testing.T) {
	var ran []string
	reg := operatorReg(t, &ran)
	forwardDaemonOwnedCommands(reg, "inst", func(context.Context, string, []string) (string, bool, error) {
		return "", false, nil
	})

	out, err := reg.Dispatch(context.Background(), []string{"session", "close", "--name", "main"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out != "local close" || len(ran) != 1 {
		t.Fatalf("out = %q ran = %v, want the local fallback", out, ran)
	}
}

// Once the daemon has answered, its answer is the answer: a failure there must
// not be retried locally against a state the daemon owns.
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

// seed carries a turn identity this process already resolved and forwards itself;
// and nothing outside `session` is the daemon's business.
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

// The rest of the invocation has to survive the trip: an unparsed remainder and
// the global --json flag are part of what the operator asked for.
func TestTheWholeInvocationSurvivesTheTrip(t *testing.T) {
	var sent []string
	reg := &cli.Registry{}
	if err := reg.Add(contracts.New("session", "create").
		Param("name", "session name", true).
		ValueParam("model", "model id", false).
		Do(func(context.Context, contracts.Input) (string, error) { return "", nil })); err != nil {
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
