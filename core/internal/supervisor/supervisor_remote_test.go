package supervisor

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func remoteSupervisor(t *testing.T) *Supervisor {
	t.Helper()
	s := NewSupervisor(context.Background(), "/usr/local/bin/herrscher")
	s.SetInstanceID("inst")
	s.SetCommandSocket("/tmp/herrscher-command-inst.sock")
	s.SetHostLookup(func(name string) (Placement, bool) {
		if name == "build1" {
			return Placement{SSH: "me@build1", Bin: "/home/me/.herrscher/bin/herrscher"}, true
		}
		return Placement{}, false
	})
	return s
}

func TestRemoteBridgeRunsOverSSHWithTheRemoteBinary(t *testing.T) {
	s := remoteSupervisor(t)
	cmd, err := s.bridgeCommand(context.Background(), state.Session{
		Name: "s1", ChannelID: "c1", Cmd: "claude", Host: "build1", Dir: "/srv/work/proj/.herrscher-sessions/inst/s1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args[0] != "ssh" {
		t.Fatalf("argv[0] = %q", cmd.Args[0])
	}
	joined := strings.Join(cmd.Args, " ")
	script := cmd.Args[len(cmd.Args)-1]
	if !strings.Contains(script, "cd '/srv/work/proj/.herrscher-sessions/inst/s1'") {
		t.Fatalf("script does not cd into the remote worktree: %s", script)
	}
	if !strings.Contains(script, "'/home/me/.herrscher/bin/herrscher' 'bridge'") {
		t.Fatalf("script does not run the remote binary: %s", script)
	}
	if !strings.Contains(script, "'--env-stdin'") {
		t.Fatalf("a remote bridge must be told to read its environment from stdin: %s", script)
	}
	// The hub socket the bridge dials is the one the forward exposes, not this
	// machine's path.
	if !strings.Contains(script, "'--hub-socket' '/tmp/herrscher-control-s1.sock'") {
		t.Fatalf("hub socket is not the remote path: %s", script)
	}
	if !strings.Contains(joined, "-R /tmp/herrscher-control-s1.sock:") {
		t.Fatalf("the control socket is not forwarded: %s", joined)
	}
	// The <capabilities> block promises the agent it can run `herrscher <verb>`.
	// On another machine that is only true if the command socket followed.
	if !strings.Contains(joined, "-R /tmp/herrscher-command-s1.sock:/tmp/herrscher-command-inst.sock") {
		t.Fatalf("the command socket is not forwarded: %s", joined)
	}
	if !strings.Contains(joined, "ExitOnForwardFailure=yes") {
		t.Fatalf("a failed forward would not kill the launch: %s", joined)
	}
	// A launch owns its connection. Over a shared master the forwards would
	// belong to the master, and the next launch asking for the same path would
	// get silence instead of a socket.
	if !strings.Contains(joined, "ControlPath=none") || !strings.Contains(joined, "ControlMaster=no") {
		t.Fatalf("the launch multiplexes its connection: %s", joined)
	}
	// cmd.Dir belongs to the far machine and is in the script; setting it here
	// would move the local ssh process instead.
	if cmd.Dir != "" {
		t.Fatalf("Dir = %q, want empty", cmd.Dir)
	}
}

// The rule that made the local test exist holds over ssh, on both machines.
func TestRemoteBridgeArgvNeverCarriesTheGatewayToken(t *testing.T) {
	s := remoteSupervisor(t)
	s.SetBridgeEnv([]string{gatewayURLVar + "=https://gw", gatewayTokenVar + "=" + secretToken})
	cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: "s1", ChannelID: "c1", Cmd: "claude", Host: "build1"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	for _, secret := range []string{secretToken, gatewayTokenVar, gatewayURLVar} {
		if strings.Contains(joined, secret) {
			t.Fatalf("argv exposes %q: %s", secret, joined)
		}
	}
}

func TestRemoteBridgeSendsItsEnvironmentOnStdin(t *testing.T) {
	s := remoteSupervisor(t)
	s.SetBridgeEnv([]string{gatewayURLVar + "=https://gw", gatewayTokenVar + "=" + secretToken})
	cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: "s1", ChannelID: "c1", Cmd: "claude", Host: "build1"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Stdin == nil {
		t.Fatal("no stdin: the remote bridge would start without its credentials")
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, cmd.Stdin); err != nil {
		t.Fatal(err)
	}
	block := buf.String()
	if !strings.Contains(block, gatewayTokenVar+"="+secretToken+"\n") {
		t.Fatalf("stdin block misses the token: %q", block)
	}
	if !strings.HasSuffix(block, "\n\n") {
		t.Fatalf("the block is not terminated by a blank line: %q", block)
	}
	// The remote children resolve the command socket from these two.
	if !strings.Contains(block, "HERRSCHER_INSTANCE_ID=inst\n") {
		t.Fatalf("stdin block misses the instance id: %q", block)
	}
	if !strings.Contains(block, "TMPDIR=/tmp\n") {
		t.Fatalf("stdin block misses TMPDIR: %q", block)
	}
	// And the third: the per-session path the forward bound, which nothing over
	// there could derive from the two above.
	if !strings.Contains(block, control.CommandSocketVar+"=/tmp/herrscher-command-s1.sock\n") {
		t.Fatalf("stdin block misses the command socket path: %q", block)
	}
}

// Two sessions on one host is the ordinary case, and it is where a single
// daemon-wide path breaks: the second launch would find it bound and die on
// ExitOnForwardFailure, and clearing it first would cut the first agent off from
// the daemon. Both forwards must name the same socket here and a different one
// over there.
func TestTwoSessionsOnOneHostGetDistinctCommandSockets(t *testing.T) {
	s := remoteSupervisor(t)
	for _, name := range []string{"s1", "s2"} {
		cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: name, ChannelID: "c", Cmd: "claude", Host: "build1"})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(cmd.Args, " ")
		if !strings.Contains(joined, "-R "+control.RemoteCommandSocketPath(name)+":/tmp/herrscher-command-inst.sock") {
			t.Fatalf("session %s does not forward its own command socket back to this daemon: %s", name, joined)
		}
	}
	if control.RemoteCommandSocketPath("s1") == control.RemoteCommandSocketPath("s2") {
		t.Fatal("two sessions share one remote command socket path")
	}
}

// No fallback to local. Running here an agent the operator believed was
// elsewhere is exactly the silence routing exists to prevent.
func TestAnUnknownHostRefusesRatherThanRunHere(t *testing.T) {
	s := remoteSupervisor(t)
	_, err := s.bridgeCommand(context.Background(), state.Session{Name: "s1", ChannelID: "c1", Cmd: "claude", Host: "ghost"})
	if err == nil {
		t.Fatal("want a refusal")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("the refusal does not name the host: %v", err)
	}
}

// Non-regression: a session with no host is still a local subprocess.
func TestALocalSessionIsUnchanged(t *testing.T) {
	s := remoteSupervisor(t)
	cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: "s1", ChannelID: "c1", Cmd: "claude", Dir: "/here"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args[0] == "ssh" {
		t.Fatalf("a local session went over ssh: %v", cmd.Args)
	}
	if cmd.Dir != "/here" {
		t.Fatalf("Dir = %q", cmd.Dir)
	}
	if cmd.Stdin != nil {
		t.Fatal("a local bridge must keep reading nothing on stdin")
	}
}
