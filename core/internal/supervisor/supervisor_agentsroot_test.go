package supervisor

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestBridgeArgsIncludesAgentsRootWhenSet(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	s.SetAgentsRoot("/custom/state/agents")
	args := s.bridgeArgs(state.Session{Name: "main", ChannelID: "c1", Cmd: "run"})
	if !strings.Contains(strings.Join(args, " "), "--agents-root /custom/state/agents") {
		t.Fatalf("bridgeArgs must pass --agents-root; got %v", args)
	}
}

func TestBridgeArgsOmitsAgentsRootWhenUnset(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	args := s.bridgeArgs(state.Session{Name: "main", ChannelID: "c1", Cmd: "run"})
	if strings.Contains(strings.Join(args, " "), "--agents-root") {
		t.Fatalf("no --agents-root expected when unset; got %v", args)
	}
}
