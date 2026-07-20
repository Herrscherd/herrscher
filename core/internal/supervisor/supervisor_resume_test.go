package supervisor

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestBridgeArgsIncludesResume(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	args := s.bridgeArgs(state.Session{Name: "main", ChannelID: "c1", Cmd: "run", ResumeToken: "sid-1"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--resume sid-1") {
		t.Fatalf("bridgeArgs must pass --resume sid-1; got %q", joined)
	}
}

func TestBridgeArgsOmitsResumeWhenEmpty(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher")
	args := s.bridgeArgs(state.Session{Name: "main", ChannelID: "c1", Cmd: "run"})
	if strings.Contains(strings.Join(args, " "), "--resume") {
		t.Fatalf("no --resume expected for empty token; got %v", args)
	}
}
