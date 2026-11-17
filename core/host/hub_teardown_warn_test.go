package host

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestAStuckTeardownWarnsOnceForItsWindow(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "slow", ChannelID: "c1", Incarnation: "slow"}); err != nil {
		t.Fatal(err)
	}
	var warnings lockedBuffer
	h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages()), withWarnings(&warnings))
	h.runtime = isolatedRuntimeDir(t)
	h.teardownGrace = 30 * time.Millisecond
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	h.stopBridge = func(string) error {
		<-release
		return nil
	}
	h.live["slow"] = liveSession{
		cancel:  func() {},
		done:    make(chan struct{}),
		release: func() {},
	}

	h.goDead("slow")
	awaitSinkQuiet(t, h.sink)

	if got := strings.Count(warnings.String(), "\n"); got != 1 {
		t.Fatalf("a stuck teardown wrote %d overrun warnings, want 1:\n%s", got, warnings.String())
	}
}
