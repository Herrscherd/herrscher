package host

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionArchivedDuringReconcileStaysDown(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	for _, name := range []string{"a", "b"} {
		if err := st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
			t.Fatal(err)
		}
	}
	h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages()))
	h.runtime = isolatedRuntimeDir(t)
	var first, second string
	h.startBridge = func(sess state.Session, _ time.Duration) error {
		if first != "" {
			return nil
		}
		first, second = sess.Name, "a"
		if sess.Name == "a" {
			second = "b"
		}
		return st.SetArchived(second, true)
	}
	h.stopBridge = func(string) error { return nil }
	t.Cleanup(h.stopLive)

	h.reconcile(t.Context())

	if !h.isLive(first) {
		t.Fatalf("session %s, started first, is not live", first)
	}
	if h.isLive(second) {
		t.Fatalf("session %s was archived during reconcile yet came live", second)
	}
}
