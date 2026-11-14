package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestResumeWhoseArchiveFlagCannotBePersistedStartsNoBridge(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	name := "persist-fail-" + newTurnID()
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(statePath)
	if err := st.AddSession(state.Session{Name: name, ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetArchived(name, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	sup := supervisor.NewSupervisor(ctx, filepath.Join(t.TempDir(), "no-such-bridge"), layout)
	h := newHub(st, sup, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable(), withWarnings(io.Discard))
	h.runtime = layout
	h.teardownGrace = defaultTeardownGrace
	var runningAtStop []bool
	h.stopBridge = func(stopping string) error {
		runningAtStop = append(runningAtStop, sup.Running(stopping))
		return sup.Stop(stopping)
	}

	if err := h.Resume(name); err == nil {
		t.Fatal("Resume succeeded although the archived flag could not be persisted")
	}
	if h.isLive(name) {
		t.Fatal("the session stayed live after its persistence failed")
	}
	if slices.Contains(runningAtStop, true) {
		t.Fatal("a bridge was started before the persistence failed: the flag must be persisted before the bridge starts")
	}
}
