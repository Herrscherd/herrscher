package supervisor

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func bridgeEnvMap(t *testing.T, s *Supervisor, sess state.Session) map[string]string {
	t.Helper()
	cmd, err := s.bridgeCommand(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}
	env := make(map[string]string)
	for _, kv := range cmd.Env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

func TestBridgeCommandCarriesApprovalsForABackendThatGates(t *testing.T) {
	s := NewSupervisor(context.Background(), "/opt/herrscher")
	s.SetGateResolver(func(string) string { return "tool" })

	env := bridgeEnvMap(t, s, state.Session{Name: "api", ChannelID: "c1", Vendor: "codex", Approvals: "strict"})
	if env[contracts.EnvApprovalsMode] != "strict" {
		t.Fatalf("mode = %q, want strict", env[contracts.EnvApprovalsMode])
	}
	if env[contracts.EnvApprovalsSession] != "api" {
		t.Fatalf("session = %q, want api", env[contracts.EnvApprovalsSession])
	}
	if env[contracts.EnvApprovalsBin] != "/opt/herrscher" {
		t.Fatalf("binary = %q, want the supervising herrscher", env[contracts.EnvApprovalsBin])
	}
}

func TestBridgeCommandExportsTheSessionNameOnce(t *testing.T) {
	s := NewSupervisor(context.Background(), "/opt/herrscher")
	s.SetGateResolver(func(string) string { return "tool" })

	cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: "api", ChannelID: "c1", Vendor: "codex", Approvals: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, contracts.EnvApprovalsSession+"=") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%s appears %d times, want 1", contracts.EnvApprovalsSession, n)
	}
}

func TestBridgeCommandOmitsApprovalsWhenNothingWouldEnforceThem(t *testing.T) {
	cases := []struct {
		name, grain, mode string
	}{
		{"vendor cannot gate", "", "strict"},
		{"operator chose bypass", "tool", "bypass"},
		{"no mode was asked for", "tool", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewSupervisor(context.Background(), "/opt/herrscher")
			s.SetGateResolver(func(string) string { return c.grain })
			env := bridgeEnvMap(t, s, state.Session{Name: "api", ChannelID: "c1", Vendor: "cursor", Approvals: c.mode})
			if _, ok := env[contracts.EnvApprovalsMode]; ok {
				t.Fatalf("carried a mode: %q", env[contracts.EnvApprovalsMode])
			}
			if _, ok := env[contracts.EnvApprovalsBin]; ok {
				t.Fatal("carried a binary to ask with")
			}
		})
	}
}

func TestBridgeCommandWithNoResolverCarriesNoApprovals(t *testing.T) {
	s := NewSupervisor(context.Background(), "/opt/herrscher")
	env := bridgeEnvMap(t, s, state.Session{Name: "api", ChannelID: "c1", Vendor: "codex", Approvals: "strict"})
	if _, ok := env[contracts.EnvApprovalsMode]; ok {
		t.Fatal("an unwired daemon claimed to gate")
	}
}

func TestRemoteEnvBlockNamesTheBinaryOverThere(t *testing.T) {
	s := NewSupervisor(context.Background(), "/opt/herrscher")
	s.SetGateResolver(func(string) string { return "tool" })
	s.SetHostLookup(func(_ string) (Placement, bool) {
		return Placement{SSH: "me@build1", Bin: "/remote/bin/herrscher"}, true
	})

	block := s.remoteEnvBlock(state.Session{Name: "api", ChannelID: "c1", Vendor: "codex", Approvals: "strict", Host: "build1"})
	env := contracts.ParseEnvSetting(block)
	if env[contracts.EnvApprovalsBin] != "/remote/bin/herrscher" {
		t.Fatalf("binary = %q, want the one on the far machine", env[contracts.EnvApprovalsBin])
	}
	if env[contracts.EnvApprovalsMode] != "strict" {
		t.Fatalf("mode = %q", env[contracts.EnvApprovalsMode])
	}
}
