package supervisor

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestALocalBridgeIsPreparedBeforeEachLaunch(t *testing.T) {
	s := NewSupervisor(context.Background(), "/bin/true", testLayout(t))
	var prepared []string
	s.SetLocalPrepare(func(_ context.Context, sess state.Session) { prepared = append(prepared, sess.Worktree) })
	for range 2 {
		s.runBridgeCommand(t.Context(), state.Session{Name: "worker", Worktree: "/work/tree"})
	}
	if len(prepared) != 2 || prepared[0] != "/work/tree" {
		t.Fatalf("prepared = %q, want the worktree readied before each of the two launches", prepared)
	}
}
