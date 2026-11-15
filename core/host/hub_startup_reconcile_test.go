package host

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

const startupLockProbe = 200 * time.Millisecond

func TestStartupReconcileWaitsForTheDispatchLock(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable())
	h.dispatchMu.Lock()
	returned := make(chan struct{})
	go func() {
		h.startReconcile(t.Context())
		close(returned)
	}()
	select {
	case <-returned:
		h.dispatchMu.Unlock()
		t.Fatal("the startup reconcile ran while a dispatch held the lock: a command socket already served could reconcile at the same time")
	case <-time.After(startupLockProbe):
	}
	h.dispatchMu.Unlock()
	<-returned
}
