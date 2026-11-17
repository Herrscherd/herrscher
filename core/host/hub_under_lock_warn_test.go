package host

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestAStuckUnderLockStopWarnsOncePerSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
		names := []string{"warn-a", "warn-b", "warn-c"}
		var warnings lockedBuffer
		h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages()), withWarnings(&warnings))
		h.runtime = isolatedRuntimeDir(t)
		h.teardownGrace = reconcileGrace
		release := make(chan struct{})
		defer close(release)
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
			h.live[name] = liveSession{cancel: func() {}, done: make(chan struct{}), release: func() {}}
		}

		batch := h.reconcile(t.Context())
		batch.await(h)

		awaitSinkQuiet(t, h.sink)
		out := warnings.String()
		if lines := strings.Count(out, "\n"); lines != len(names) {
			t.Fatalf("three stuck stops of sessions archived during start wrote %d overrun lines, want one per session (%d):\n%s", lines, len(names), out)
		}
		for _, name := range names {
			if n := strings.Count(out, name); n != 1 {
				t.Fatalf("session %s named %d times in the overrun warnings, want 1:\n%s", name, n, out)
			}
		}
	})
}
