package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

var errTerminalAccept = errors.New("accept: the listener failed for good")

type brokenListener struct{}

func (brokenListener) Accept() (net.Conn, error) { return nil, errTerminalAccept }

func (brokenListener) Close() error { return nil }

func (brokenListener) Addr() net.Addr { return nil }

func brokenFirstListen() func(context.Context, string) (net.Listener, error) {
	var calls atomic.Int32
	return func(ctx context.Context, path string) (net.Listener, error) {
		if calls.Add(1) == 1 {
			return brokenListener{}, nil
		}
		return control.Listen(ctx, path)
	}
}

func awaitWarning(t *testing.T, warnings *lockedBuffer, want string) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for !strings.Contains(warnings.String(), want) {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("no warning mentioning %q within 5s: %q", want, warnings.String())
		}
	}
}

func TestTerminalAcceptErrorRetiresTheSessionAndRebinds(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	name := "accept-failure-" + newTurnID()
	warnings := &lockedBuffer{}
	h := newHub(
		state.NewState(filepath.Join(t.TempDir(), "state.json")),
		nil,
		nil,
		t.TempDir(),
		nil,
		nil,
		newApprovalTable(io.Discard),
		newSessionTable(),
		withWarnings(warnings),
	)
	h.runtime = layout
	h.teardownGrace = time.Minute
	h.listen = brokenFirstListen()
	t.Cleanup(func() { h.goDead(name) })

	if err := h.goLive(t.Context(), state.Session{Name: name, Incarnation: "first"}); err != nil {
		t.Fatal(err)
	}
	awaitWarning(t, warnings, errTerminalAccept.Error())
	h.mu.Lock()
	_, stillLive := h.live[name]
	h.mu.Unlock()
	if stillLive {
		t.Fatal("the session stayed in the live table after its control socket failed for good")
	}
	out := warnings.String()
	if !strings.Contains(out, name) || !strings.Contains(out, errTerminalAccept.Error()) {
		t.Fatalf("the terminal accept error was not reported to the operator with its session: %q", out)
	}

	if err := h.goLive(t.Context(), state.Session{Name: name, Incarnation: "second"}); err != nil {
		t.Fatalf("goLive after a terminal accept error: %v", err)
	}
	conn, err := control.Dial(control.SocketPath(layout, "", name))
	if err != nil {
		t.Fatalf("the control socket does not listen again after goLive: %v", err)
	}
	logCleanup(conn.Close())
}

func TestNormalStopReportsNothing(t *testing.T) {
	stops := map[string]func(*hub, string){
		"goDead":   func(h *hub, name string) { h.goDead(name) },
		"stopLive": func(h *hub, _ string) { h.stopLive() },
	}
	for stop, run := range stops {
		t.Run(stop, func(t *testing.T) {
			layout := isolatedRuntimeDir(t)
			name := "normal-stop-" + newTurnID()
			warnings := &lockedBuffer{}
			h := newHub(
				state.NewState(filepath.Join(t.TempDir(), "state.json")),
				nil,
				nil,
				t.TempDir(),
				nil,
				nil,
				newApprovalTable(io.Discard),
				newSessionTable(),
				withWarnings(warnings),
			)
			h.runtime = layout
			t.Cleanup(func() { h.goDead(name) })

			if err := h.goLive(t.Context(), state.Session{Name: name, Incarnation: "only"}); err != nil {
				t.Fatal(err)
			}
			run(h, name)
			if out := warnings.String(); out != "" {
				t.Fatalf("a normal stop through %s reported %q, want nothing", stop, out)
			}
		})
	}
}

func TestStaleAcceptFailureLeavesTheReplacementLive(t *testing.T) {
	name := "stale-" + newTurnID()
	current := make(chan struct{})
	stale := make(chan struct{})
	var warnings bytes.Buffer
	h := &hub{warnings: &warnings, live: map[string]liveSession{name: {done: current, cancel: func() {}, release: func() {}}}}

	h.acceptFailed(name, stale, errTerminalAccept)

	h.mu.Lock()
	replacement, stillLive := h.live[name]
	h.mu.Unlock()
	if !stillLive || replacement.done != current {
		t.Fatal("a late accept failure of a retired incarnation removed its replacement")
	}
	if out := warnings.String(); out != "" {
		t.Fatalf("a late accept failure of a retired incarnation reported %q, want nothing", out)
	}
}
