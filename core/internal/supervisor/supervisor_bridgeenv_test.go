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

func TestBridgeCommandCarriesTheGatewayPairInItsEnvironment(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	s.SetBridgeEnv([]string{gatewayURLVar + "=https://gw", gatewayTokenVar + "=" + secretToken})

	cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: "main", ChannelID: "c1", Cmd: "claude"})
	if err != nil {
		t.Fatal(err)
	}
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

func TestBridgeArgvNeverCarriesTheGatewayToken(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	s.SetBridgeEnv([]string{gatewayURLVar + "=https://gw", gatewayTokenVar + "=" + secretToken})

	sess := state.Session{
		Name: "main", ChannelID: "c1", Cmd: "claude", Vendor: "claude", ModelID: "gw-claude-opus-5",
	}
	cmd, err := s.bridgeCommand(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(append([]string{cmd.Path}, cmd.Args...), " ")
	for _, secret := range []string{secretToken, gatewayTokenVar, gatewayURLVar} {
		if strings.Contains(argv, secret) {
			t.Fatalf("argv exposes %q: %s", secret, argv)
		}
	}
	if strings.Contains(sess.Cmd, secretToken) {
		t.Fatalf("the persisted Cmd carries the token: %q", sess.Cmd)
	}
}

func TestBridgeCommandInheritsEnvironmentWithoutExtras(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: "main", ChannelID: "c1", Cmd: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd.Env) == 0 {
		t.Fatal("bridge child got an empty environment; it must inherit the daemon's")
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, gatewayTokenVar+"=") {
			t.Fatalf("unexpected gateway token in the inherited environment: %q", kv)
		}
	}
}
