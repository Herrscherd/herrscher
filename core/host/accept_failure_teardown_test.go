package host

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestGoDeadWaitsForAnAcceptFailureStillRetiring(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	name := "accept-teardown-" + newTurnID()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: name, ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	sup := supervisor.NewSupervisor(ctx, filepath.Join(t.TempDir(), "no-such-bridge"), layout)
	warnings := &lockedBuffer{}
	h := newHub(st, sup, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages()), withWarnings(warnings))
	h.runtime = layout
	h.teardownGrace = time.Minute
	h.listen = brokenFirstListen()

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHalt := func() { releaseOnce.Do(func() { close(release) }) }
	var holdOnce sync.Once
	h.stopBridge = func(string) error {
		holdOnce.Do(func() {
			close(entered)
			<-release
		})
		return nil
	}
	t.Cleanup(func() { h.goDead(name) })
	t.Cleanup(releaseHalt)

	if err := h.goLive(t.Context(), state.Session{Name: name, ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the failed session was never retired")
	}

	dead := make(chan struct{})
	go func() {
		defer close(dead)
		h.goDead(name)
	}()
	select {
	case <-dead:
		t.Fatal("goDead returned while the accept failure of the session was still retiring")
	case <-time.After(100 * time.Millisecond):
	}

	releaseHalt()
	select {
	case <-dead:
	case <-time.After(5 * time.Second):
		t.Fatal("goDead did not return once the accept failure finished")
	}
	if !strings.Contains(warnings.String(), errTerminalAccept.Error()) {
		t.Fatalf("goDead returned before the accept failure was reported: %q", warnings.String())
	}
}
