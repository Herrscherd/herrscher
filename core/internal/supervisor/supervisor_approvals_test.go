package supervisor

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// bridgeEnvMap decodes the child environment into a map so a test can ask about
// one variable without caring what else rides along.
func bridgeEnvMap(t *testing.T, s *Supervisor, sess state.Session) map[string]string {
	t.Helper()
	cmd, err := s.bridgeCommand(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
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

// EnvApprovalsSession is HERRSCHER_SESSION, which the launch already exports.
// The merge must leave exactly one entry: an exec.Cmd holding two for one name
// has platform-dependent behaviour, so a duplicate is a bug that would only
// show up somewhere else.
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

// A backend that enforces nothing must never be handed a variable implying it
// does, and neither must a session the operator explicitly told to run ungated.
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

// A remote session must be gated by the herrscher that runs it. Sending this
// machine's path would name a binary that is not there, and every ask would
// fail, which under the failure contract means every tool call is allowed.
func TestRemoteEnvBlockNamesTheBinaryOverThere(t *testing.T) {
	s := NewSupervisor(context.Background(), "/opt/herrscher")
	s.SetGateResolver(func(string) string { return "tool" })
	s.SetHostLookup(func(name string) (Placement, bool) {
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
