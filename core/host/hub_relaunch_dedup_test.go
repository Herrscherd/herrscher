package host

import (
	"errors"
	"io"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestSettledStopStartsEachStuckSessionOnce(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "stuck", ChannelID: "c1", Incarnation: "stuck"}); err != nil {
		t.Fatal(err)
	}
	h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages()))
	h.runtime = isolatedRuntimeDir(t)
	settled := make(chan struct{})
	stillStopping := true
	h.stopping = func(string) <-chan struct{} {
		if stillStopping {
			return settled
		}
		return nil
	}
	var starts atomic.Int32
	h.startBridge = func(state.Session, time.Duration) error {
		starts.Add(1)
		return nil
	}
	h.stopBridge = func(string) error { return nil }
	t.Cleanup(h.stopLive)

	sess, _ := st.FindSession("stuck")
	for range 2 {
		if err := h.goLiveBridged(t.Context(), sess); !errors.Is(err, supervisor.ErrStillStopping) {
			t.Fatalf("start beside a stopping bridge = %v, want ErrStillStopping", err)
		}
	}
	stillStopping = false
	close(settled)
	<-h.relaunches.settled()
	if got := starts.Load(); got != 1 {
		t.Fatalf("the bridge was started %d times after its stop settled, want 1", got)
	}
}
