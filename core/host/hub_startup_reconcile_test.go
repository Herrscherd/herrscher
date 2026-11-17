package host

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestStartupReconcileBridgesUnderTheDispatchLock(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "resumed", ChannelID: "c1", Incarnation: "resumed"}); err != nil {
		t.Fatal(err)
	}
	h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages()))
	h.runtime = isolatedRuntimeDir(t)
	t.Cleanup(h.stopLive)
	var dispatchFreeWhileBridging []bool
	h.startBridge = func(state.Session, time.Duration) error {
		acquired := h.dispatchMu.TryLock()
		if acquired {
			h.dispatchMu.Unlock()
		}
		dispatchFreeWhileBridging = append(dispatchFreeWhileBridging, acquired)
		return nil
	}
	h.startReconcile(t.Context())
	if len(dispatchFreeWhileBridging) == 0 {
		t.Fatal("the startup reconcile never bridged the resumed session: the probe did not run")
	}
	for _, free := range dispatchFreeWhileBridging {
		if free {
			t.Fatal("the startup reconcile bridged a session while no dispatch held the lock: a command socket already served could reconcile at the same time")
		}
	}
}
