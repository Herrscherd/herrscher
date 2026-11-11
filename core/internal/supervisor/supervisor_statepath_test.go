package supervisor

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestLocalBridgeReceivesTheDaemonStatePath(t *testing.T) {
	s := NewSupervisor(context.Background(), "herrscher", testLayout(t))
	s.SetStatePath("/daemon/state.json")

	cmd, err := s.bridgeCommand(context.Background(), state.Session{Name: "main", ChannelID: "c1", Cmd: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for idx := 0; idx+1 < len(cmd.Args); idx++ {
		if cmd.Args[idx] == "--state" && cmd.Args[idx+1] == "/daemon/state.json" {
			return
		}
	}
	t.Fatalf("bridge args %v lack --state /daemon/state.json", cmd.Args)
}
