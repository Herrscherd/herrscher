package supervisor

import (
	"context"
	"errors"
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

func TestRemoteRuntimeDirIsResolvedOncePerHost(t *testing.T) {
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
	if calls != 1 {
		t.Fatalf("remote runtime dir resolved %d times for one host, want once for the session", calls)
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
