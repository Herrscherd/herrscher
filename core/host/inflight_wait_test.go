package host

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

type inflightPath struct {
	name string
	run  func(h *hub, session string)
}

func inflightPaths() []inflightPath {
	return []inflightPath{
		{name: "goDead", run: func(h *hub, session string) { h.goDead(session) }},
		{name: "stopLive", run: func(h *hub, _ string) { h.stopLive() }},
		{name: "closeAndRemoveSockets", run: func(h *hub, _ string) { logCleanup(h.closeAndRemoveSockets()) }},
	}
}

type gatedWarnings struct {
	marker    string
	entered   chan struct{}
	release   chan struct{}
	enterOnce sync.Once
	openOnce  sync.Once
	out       lockedBuffer
}

func newGatedWarnings(marker string) *gatedWarnings {
	return &gatedWarnings{marker: marker, entered: make(chan struct{}), release: make(chan struct{})}
}

func (g *gatedWarnings) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(g.marker)) {
		g.enterOnce.Do(func() { close(g.entered) })
		<-g.release
	}
	return g.out.Write(p)
}

func (g *gatedWarnings) open() {
	g.openOnce.Do(func() { close(g.release) })
}

func startAcceptFailure(t *testing.T, grace time.Duration, warnings io.Writer) (*hub, string) {
	t.Helper()
	layout := isolatedRuntimeDir(t)
	name := "inflight-" + newTurnID()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: name, ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	sup := supervisor.NewSupervisor(ctx, filepath.Join(t.TempDir(), "no-such-bridge"), layout)
	h := newHub(st, sup, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable(), withWarnings(warnings))
	h.runtime = layout
	h.teardownGrace = grace
	h.listen = brokenFirstListen()
	if err := h.goLive(t.Context(), state.Session{Name: name, ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	return h, name
}

func awaitGatedWrite(t *testing.T, warnings *gatedWarnings) {
	t.Helper()
	select {
	case <-warnings.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the accept failure warning was never written")
	}
}

func runInBackground(run func()) <-chan struct{} {
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		run()
	}()
	return returned
}

func TestStopWaitsForTheAcceptFailureWarningToBeWritten(t *testing.T) {
	for _, path := range inflightPaths() {
		t.Run(path.name, func(t *testing.T) {
			warnings := newGatedWarnings(errTerminalAccept.Error())
			t.Cleanup(warnings.open)
			h, name := startAcceptFailure(t, time.Minute, warnings)
			awaitGatedWrite(t, warnings)

			returned := runInBackground(func() { path.run(h, name) })
			select {
			case <-returned:
				t.Fatal("returned before the accept failure warning was written")
			case <-time.After(100 * time.Millisecond):
			}

			warnings.open()
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Fatal("did not return once the accept failure warning was written")
			}
			if !strings.Contains(warnings.out.String(), errTerminalAccept.Error()) {
				t.Fatalf("returned without the accept failure warning: %q", warnings.out.String())
			}
		})
	}
}

func TestBlockedWarningWriterDoesNotHoldStopPastTheBudget(t *testing.T) {
	for _, path := range inflightPaths() {
		t.Run(path.name, func(t *testing.T) {
			warnings := newGatedWarnings(errTerminalAccept.Error())
			t.Cleanup(warnings.open)
			h, name := startAcceptFailure(t, 300*time.Millisecond, warnings)
			awaitGatedWrite(t, warnings)

			returned := runInBackground(func() { path.run(h, name) })
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Fatal("a blocked warning writer held the stop past the teardown budget")
			}
			overrun := newHostMessages().text(h.textLanguage()(), "warn.hub_teardown_overrun", map[string]any{graceParam: h.teardownGrace.String()})
			awaitWarning(t, &warnings.out, overrun)
		})
	}
}
