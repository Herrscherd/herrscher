package host

import (
	"errors"
	"io"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestRefusedResumeIsRelaunchedByTheNextReconcile(t *testing.T) {
	name := "refused-resume-" + newTurnID()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: name, ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	h := newHub(st, nil, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable())
	h.runtime = isolatedRuntimeDir(t)
	var starts, stops atomic.Int32
	h.startBridge = func(state.Session, time.Duration) error {
		if starts.Add(1) == 1 {
			return supervisor.ErrStillStopping
		}
		return nil
	}
	h.stopBridge = func(string) error {
		stops.Add(1)
		return nil
	}
	t.Cleanup(func() { h.goDead(name) })

	if err := h.Resume(name); !errors.Is(err, supervisor.ErrStillStopping) {
		t.Fatalf("Resume = %v, want ErrStillStopping", err)
	}
	if h.isLive(name) {
		t.Fatal("a refused resume left the session live")
	}
	h.reconcile(t.Context())
	if !h.isLive(name) {
		t.Fatal("the next reconcile did not bring the refused session back")
	}
	if got := starts.Load(); got < 2 {
		t.Fatalf("starts = %d after reconcile, want the bridge relaunched (>= 2)", got)
	}
	if got := stops.Load(); got != 0 {
		t.Fatalf("stops = %d, want no bridge stop for a refused start", got)
	}
}

func TestAcceptFailureRetiredBridgeIsRelaunchedByReconcile(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	name := "accept-relaunch-" + newTurnID()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: name, ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	warnings := &lockedBuffer{}
	h := newHub(st, nil, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable(), withWarnings(warnings))
	h.runtime = layout
	h.teardownGrace = time.Minute
	h.listen = brokenFirstListen()
	var starts, stops atomic.Int32
	h.startBridge = func(state.Session, time.Duration) error {
		starts.Add(1)
		return nil
	}
	h.stopBridge = func(string) error {
		stops.Add(1)
		return nil
	}
	t.Cleanup(func() { h.goDead(name) })

	if err := h.goLiveBridged(t.Context(), state.Session{Name: name, ChannelID: "c1", Incarnation: "first"}); err != nil {
		t.Fatal(err)
	}
	awaitWarning(t, warnings, errTerminalAccept.Error())
	if h.isLive(name) {
		t.Fatal("the session stayed live after its control socket failed for good")
	}
	if got := stops.Load(); got != 1 {
		t.Fatalf("stops = %d after the accept failure, want the bridge retired once", got)
	}

	h.reconcile(t.Context())
	if !h.isLive(name) {
		t.Fatal("reconcile did not bring the retired session back")
	}
	if got := starts.Load(); got < 2 {
		t.Fatalf("starts = %d after reconcile, want the bridge relaunched (>= 2)", got)
	}
}
