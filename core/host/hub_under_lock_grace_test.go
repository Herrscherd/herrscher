package host

import (
	"io"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestUnderLockRetirementsShareOneGraceInReconcile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
		names := []string{"start-a", "start-b", "start-c"}
		h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable())
		h.runtime = isolatedRuntimeDir(t)
		h.teardownGrace = reconcileGrace
		release := make(chan struct{})
		defer close(release)
		defer h.closeStderr()
		h.stopBridge = func(string) error {
			<-release
			return nil
		}
		h.startBridge = func(sess state.Session, _ time.Duration) error {
			return st.SetArchived(sess.Name, true)
		}
		for _, name := range names {
			if err := st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
				t.Fatal(err)
			}
			finished := make(chan struct{})
			close(finished)
			h.live[name] = liveSession{cancel: func() {}, done: finished, release: func() {}}
		}

		started := time.Now()
		batch := h.reconcile(t.Context())
		elapsed := time.Since(started)
		batch.await(h)
		if bound := reconcileGrace + reconcileGrace/2; elapsed > bound {
			t.Fatalf("reconcile took %v retiring three sessions archived during start, want at most %v: each under-lock retirement waited its own grace", elapsed, bound)
		}
		for _, name := range names {
			if h.isLive(name) {
				t.Fatalf("session %s archived during start stayed live", name)
			}
		}
	})
}
