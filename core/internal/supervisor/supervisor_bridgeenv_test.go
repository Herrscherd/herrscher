package supervisor

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	gatewayURLVar   = "NEUBLOX_GATEWAY_URL"
	gatewayTokenVar = "NEUBLOX_GATEWAY_TOKEN"
	secretToken     = "tok-supersecret"
)

// The daemon removes the gateway pair from its own environment so it cannot
// leak into a vendor CLI. The bridge is the process that actually builds
// backends, so it must get the pair back — through its ENVIRONMENT. Without
// this hand-off every gateway spawn in a supervised session fails closed.
func TestBridgeCommandCarriesTheGatewayPairInItsEnvironment(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	s.SetBridgeEnv([]string{gatewayURLVar + "=https://gw", gatewayTokenVar + "=" + secretToken})

	cmd := s.bridgeCommand(context.Background(), state.Session{Name: "main", ChannelID: "c1", Cmd: "claude"})
	var sawURL, sawToken bool
	for _, kv := range cmd.Env {
		switch kv {
		case gatewayURLVar + "=https://gw":
			sawURL = true
		case gatewayTokenVar + "=" + secretToken:
			sawToken = true
		}
	}
	if !sawURL || !sawToken {
		t.Fatalf("bridge child environment is missing the gateway pair (url=%v token=%v)", sawURL, sawToken)
	}
}

// The token must NEVER ride argv: /proc/<pid>/cmdline is world readable, so any
// local process could read the product's shared paid credential off the bridge.
func TestBridgeArgvNeverCarriesTheGatewayToken(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	s.SetBridgeEnv([]string{gatewayURLVar + "=https://gw", gatewayTokenVar + "=" + secretToken})

	sess := state.Session{
		Name: "main", ChannelID: "c1", Cmd: "claude", Vendor: "claude", ModelID: "gw-claude-opus-5",
	}
	cmd := s.bridgeCommand(context.Background(), sess)
	argv := strings.Join(append([]string{cmd.Path}, cmd.Args...), " ")
	for _, secret := range []string{secretToken, gatewayTokenVar, gatewayURLVar} {
		if strings.Contains(argv, secret) {
			t.Fatalf("argv exposes %q: %s", secret, argv)
		}
	}
	// The persisted invocation string must stay clean too — it is written to
	// state.json and shown by `session list`.
	if strings.Contains(sess.Cmd, secretToken) {
		t.Fatalf("the persisted Cmd carries the token: %q", sess.Cmd)
	}
}

// Non-regression: with no extra environment the child still inherits the
// daemon's, exactly as before.
func TestBridgeCommandInheritsEnvironmentWithoutExtras(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	cmd := s.bridgeCommand(context.Background(), state.Session{Name: "main", ChannelID: "c1", Cmd: "claude"})
	if len(cmd.Env) == 0 {
		t.Fatal("bridge child got an empty environment; it must inherit the daemon's")
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, gatewayTokenVar+"=") {
			t.Fatalf("unexpected gateway token in the inherited environment: %q", kv)
		}
	}
}
