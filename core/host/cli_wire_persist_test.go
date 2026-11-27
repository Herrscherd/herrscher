package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

type gatedStateWriter struct {
	gate chan struct{}
	once sync.Once
}

func (g *gatedStateWriter) open() {
	g.once.Do(func() { close(g.gate) })
}

func (g *gatedStateWriter) write(name string, data []byte, perm os.FileMode) error {
	<-g.gate
	return atomicfile.Write(name, data, perm)
}

func TestWiredManagerWriteIsAwaitedByTheStopPath(t *testing.T) {
	grace := 200 * time.Millisecond
	layout := isolatedRuntimeDir(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	gate := &gatedStateWriter{gate: make(chan struct{})}
	t.Cleanup(gate.open)
	st := state.NewStateWith(statePath, gate.write)
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

	setLang := mustCommand(t, hdl.Commands(), "set", "lang")
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		_, runErr = setLang.Run(ctx, contracts.Input{Args: map[string]string{"lang": "fr"}})
	}()

	awaitPersistWriteInFlight(t, h)
	release := time.AfterFunc(300*time.Millisecond, gate.open)
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
