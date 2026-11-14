package host

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestResumeDuringAcceptFailureKeepsItsBridge(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	name := "accept-race-" + newTurnID()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: name, ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	sup := supervisor.NewSupervisor(ctx, filepath.Join(t.TempDir(), "no-such-bridge"), layout)
	var warnings bytes.Buffer
	h := newHub(st, sup, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable(), withWarnings(&warnings))
	h.runtime = layout
	h.teardownGrace = time.Minute
	h.listen = brokenFirstListen()
	t.Cleanup(func() { h.goDead(name) })

	resumed := make(chan struct{})
	var resumeErr error
	var interpose sync.Once
	stop := h.stopBridge
	h.stopBridge = func(stopping string) error {
		interpose.Do(func() {
			go func() {
				defer close(resumed)
				resumeErr = h.Resume(stopping)
			}()
			select {
			case <-resumed:
			case <-time.After(time.Second):
			}
		})
		return stop(stopping)
	}

	sup.Start(state.Session{Name: name, ChannelID: "c1"})
	if err := h.goLive(t.Context(), state.Session{Name: name, ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-resumed:
	case <-time.After(5 * time.Second):
		t.Fatal("Resume did not return once the failed incarnation was retired")
	}
	select {
	case <-resumed:
	case <-time.After(5 * time.Second):
		t.Fatal("Resume did not return once the failed incarnation was retired")
	}
	if resumeErr != nil {
		t.Fatalf("Resume: %v", resumeErr)
	}
	h.mu.Lock()
	_, live := h.live[name]
	h.mu.Unlock()
	if !live {
		t.Fatal("the resumed session is not in the live table")
	}
	if !sup.Running(name) {
		t.Fatal("the resumed session has no bridge: retiring the failed incarnation stopped the bridge Resume had started")
	}
}
