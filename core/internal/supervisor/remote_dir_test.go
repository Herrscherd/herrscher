package supervisor

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestRemoteSocketsLiveInTheRemotePrivateDir(t *testing.T) {
	s := remoteSupervisor(t)
	cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: "s1", ChannelID: "c1", Cmd: "claude", Host: "build1"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"-R " + remoteDirForTest + "/herrscher-control-inst.s1.sock:",
		"-R " + remoteDirForTest + "/herrscher-command-inst.s1.sock:",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("forward %q missing from %s", want, joined)
		}
	}
}

func TestRemoteRuntimeDirIsResolvedAtEverySessionStart(t *testing.T) {
	s := remoteSupervisor(t)
	calls := 0
	s.remoteDirOf = func(context.Context, string) (string, error) {
		calls++
		return remoteDirForTest, nil
	}
	for _, name := range []string{"s1", "s2"} {
		if _, err := s.bridgeCommand(context.Background(), state.Session{Name: name, ChannelID: "c", Cmd: "claude", Host: "build1"}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("remote runtime dir resolved %d times for two session starts, want one lookup per start", calls)
	}
}

func TestALaunchRefusesWhenTheRemoteDirCannotBePrepared(t *testing.T) {
	s := remoteSupervisor(t)
	s.remoteDirOf = func(context.Context, string) (string, error) {
		return "", runner.ErrRemoteRuntimeDir
	}
	_, err := s.bridgeCommand(context.Background(), state.Session{Name: "s1", ChannelID: "c1", Cmd: "claude", Host: "build1"})
	if !errors.Is(err, runner.ErrRemoteRuntimeDir) {
		t.Fatalf("bridgeCommand = %v, want the remote runtime dir refusal", err)
	}
}

func TestPrepareRemoteFailsTheStartWhenTheRemoteDirCannotBePrepared(t *testing.T) {
	s := remoteSupervisor(t)
	s.remoteDirOf = func(context.Context, string) (string, error) {
		return "", runner.ErrRemoteRuntimeDir
	}
	cmd, err := s.prepareRemote(context.Background(), state.Session{Name: "s1", ChannelID: "c1", Cmd: "claude", Host: "build1"})
	if !errors.Is(err, runner.ErrRemoteRuntimeDir) {
		t.Fatalf("prepareRemote = %v, want the remote runtime dir refusal", err)
	}
	if cmd != nil {
		t.Fatalf("prepareRemote built %v after a failed lookup, want no forwards cleared silently", cmd.Args)
	}
}

func TestControlPathFailureWarnsOnceAndKeepsGoingWithoutMultiplexing(t *testing.T) {
	s := remoteSupervisor(t)
	var logs strings.Builder
	s.log = slog.New(slog.NewTextHandler(&logs, nil))
	s.controlPathOf = func(string) (string, error) {
		return "", errors.New("private dir refused")
	}
	sess := state.Session{Name: "s1", ChannelID: "c1", Cmd: "claude", Host: "build1"}
	for range 2 {
		cmd, err := s.prepareRemote(context.Background(), sess)
		if err != nil {
			t.Fatal(err)
		}
		if cmd == nil || !strings.Contains(strings.Join(cmd.Args, " "), "ControlPath=none") {
			t.Fatalf("prepareRemote = %v, want a command without multiplexing", cmd)
		}
	}
	if n := strings.Count(logs.String(), "ssh multiplexing disabled"); n != 1 {
		t.Fatalf("the multiplexing warning was logged %d times, want once: %s", n, logs.String())
	}
}
