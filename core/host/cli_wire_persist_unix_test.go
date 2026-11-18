//go:build !windows

package host

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestWiredManagerWriteIsAwaitedByTheStopPath(t *testing.T) {
	grace := 200 * time.Millisecond
	layout := isolatedRuntimeDir(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(statePath)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	sup := supervisor.NewSupervisor(ctx, filepath.Join(t.TempDir(), "no-such-bridge"), layout)
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	warnings := &lockedBuffer{}
	h := newHub(st, sup, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), sessions, withWarnings(warnings))
	h.runtime = layout
	h.teardownGrace = grace

	hdl := newManagerHandler(newHostMessages(), clitext.NewMessages(), Deps{}, Options{DefaultCmd: "claude"}, t.TempDir(), st, sup, stoppingWorktrees{}, serviceUpdater{}, agent.NewStore(t.TempDir()), nil)
	wireManagerHandler(ctx, nil, io.Discard, hdl, st, layout, "wired", nil, sessions, nil)

	tmp := statePath + ".tmp"
	if err := syscall.Mkfifo(tmp, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unblockTranscript(tmp) })

	setLang := mustCommand(t, hdl.Commands(), "set", "lang")
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		_, runErr = setLang.Run(ctx, contracts.Input{Args: map[string]string{"lang": "fr"}})
	}()

	awaitPersistWriteInFlight(t, h)
	release := time.AfterFunc(300*time.Millisecond, func() { releaseBlockedTranscript(tmp) })
	t.Cleanup(func() { release.Stop() })

	started := time.Now()
	h.stopLive()
	elapsed := time.Since(started)

	if elapsed < 200*time.Millisecond {
		t.Fatalf("the stop path returned after %v, before the manager write it had to wait for", elapsed)
	}
	if bound := 3*grace + persistWriteGrace; elapsed > bound {
		t.Fatalf("the stop path took %v waiting for one manager write, want under %v", elapsed, bound)
	}
	if strings.Contains(warnings.String(), persistOverrunText(h)) {
		t.Fatalf("the manager write was released in time but the stop announced an overrun: %q", warnings.String())
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the manager command never returned after its write was released")
	}
	if runErr != nil {
		t.Fatalf("the manager command failed after its write was released: %v", runErr)
	}
}
