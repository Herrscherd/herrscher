package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
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

func newInflightHub(t *testing.T, name string, grace time.Duration, warnings io.Writer) *hub {
	t.Helper()
	layout := isolatedRuntimeDir(t)
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: name, ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	sup := supervisor.NewSupervisor(ctx, filepath.Join(t.TempDir(), "no-such-bridge"), layout)
	owner := newHostMessages()
	h := newHub(st, sup, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable(owner, clitext.NewMessages()), withWarnings(warnings))
	h.runtime = layout
	h.teardownGrace = grace
	t.Cleanup(func() {
		h.sink.Close()
		awaitSinkLog(t, h.sink)
	})
	return h
}

func startAcceptFailure(t *testing.T, grace time.Duration, warnings io.Writer) (*hub, string) {
	t.Helper()
	name := "inflight-" + newTurnID()
	h := newInflightHub(t, name, grace, warnings)
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
		t.Fatal("the warning was never written")
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

func hubOverrunText(h *hub, _ string) string {
	return newHostMessages().text(h.textLanguage()(), "warn.hub_teardown_overrun", map[string]any{graceParam: h.teardownGrace.String()})
}

func commandOverrunText(h *hub, budget time.Duration) string {
	return newHostMessages().text(h.textLanguage()(), "warn.command_handler_overrun", map[string]any{graceParam: budget.String()})
}

func sessionOverrunText(h *hub, name string) string {
	return newHostMessages().text(h.sessionLanguageSource(name)(), "warn.session_teardown_overrun", map[string]any{nameParam: name, graceParam: h.teardownGrace.String()})
}

func stuckStartupMarker(string) string {
	return stuckStartupName
}

func stuckStartupOverrunText(h *hub, _ string) string {
	return sessionOverrunText(h, stuckStartupName)
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

type blockedWriterCase struct {
	name   string
	paths  []inflightPath
	bound  time.Duration
	start  func(t *testing.T, name string, grace time.Duration, warnings *gatedWarnings) *hub
	marker func(name string) string
	want   func(h *hub, name string) string
}

func blockedWriterCases() []blockedWriterCase {
	return []blockedWriterCase{
		{
			name:  "accept failure",
			paths: inflightPaths(),
			bound: blockedStopBound(),
			start: func(t *testing.T, name string, grace time.Duration, warnings *gatedWarnings) *hub {
				t.Helper()
				h := newInflightHub(t, name, grace, warnings)
				h.listen = brokenFirstListen()
				if err := h.goLive(t.Context(), state.Session{Name: name, ChannelID: "c1"}); err != nil {
					t.Fatal(err)
				}
				awaitGatedWrite(t, warnings)
				return h
			},
		},
		{
			name:  "teardown overrun",
			paths: []inflightPath{{name: "goDead", run: func(h *hub, session string) { h.goDead(session) }}},
			bound: blockedStopBound(),
			start: func(t *testing.T, name string, grace time.Duration, warnings *gatedWarnings) *hub {
				t.Helper()
				h := newInflightHub(t, name, grace, warnings)
				if err := h.goLive(t.Context(), state.Session{Name: name, ChannelID: "c1"}); err != nil {
					t.Fatal(err)
				}
				release := make(chan struct{})
				t.Cleanup(func() { close(release) })
				h.stopBridge = func(string) error {
					<-release
					return nil
				}
				return h
			},
			want: sessionOverrunText,
		},
		{
			name:  "session start",
			paths: inflightPaths(),
			bound: blockedStopBound(),
			start: func(t *testing.T, name string, grace time.Duration, warnings *gatedWarnings) *hub {
				t.Helper()
				h := newInflightHub(t, name, grace, warnings)
				h.startBridge = func(state.Session, time.Duration) error { return errors.New("bridge refused") }
				sess := state.Session{Name: name, ChannelID: "c1"}
				sessionStart := runInBackground(func() { logCleanup(h.goLiveBridged(t.Context(), sess)) })
				t.Cleanup(func() {
					warnings.open()
					awaitBackground(t, sessionStart, "the refused session start did not return once its warning was written")
				})
				awaitGatedWrite(t, warnings)
				return h
			},
		},
		{
			name:  "startup ignoring cancel",
			paths: []inflightPath{{name: "closeAndRemoveSockets", run: func(h *hub, _ string) { logCleanup(h.closeAndRemoveSockets()) }}},
			bound: blockedStopBound(),
			start: func(t *testing.T, name string, grace time.Duration, warnings *gatedWarnings) *hub {
				t.Helper()
				h := newInflightHub(t, name, grace, warnings)
				h.listen = brokenFirstListen()
				if err := h.goLive(t.Context(), state.Session{Name: name, ChannelID: "c1"}); err != nil {
					t.Fatal(err)
				}
				h.trackStartup(startupRun{name: stuckStartupName, cancel: func() {}, done: make(chan struct{})})
				return h
			},
			marker: stuckStartupMarker,
			want:   stuckStartupOverrunText,
		},
	}
}

const (
	blockedGrace      = 300 * time.Millisecond
	blockedStopMargin = 250 * time.Millisecond
	stuckStartupName  = "stuck"
)

func blockedStopBound() time.Duration {
	return 3*blockedGrace + 7*warningWriteBudget + blockedStopMargin
}

func TestBlockedWarningWriterDoesNotHoldStopPastTheBudget(t *testing.T) {
	for _, c := range blockedWriterCases() {
		for _, path := range c.paths {
			t.Run(c.name+"/"+path.name, func(t *testing.T) {
				runBlockedWriterCase(t, c, path)
			})
		}
	}
}

func runBlockedWriterCase(t *testing.T, c blockedWriterCase, path inflightPath) {
	t.Helper()
	name := "inflight-" + newTurnID()
	logs := captureDefaultLog(t)
	warnings := newGatedWarnings(blockedWriterMarker(c, name))
	t.Cleanup(warnings.open)
	h := c.start(t, name, blockedGrace, warnings)

	started := time.Now()
	returned := runInBackground(func() { path.run(h, name) })
	t.Cleanup(func() {
		warnings.open()
		awaitBackground(t, returned, "the stop path did not return once the warning writer was released")
	})
	if c.want == nil {
		awaitGatedWrite(t, warnings)
	}
	select {
	case <-returned:
	case <-time.After(c.bound):
	}
	if elapsed := time.Since(started); elapsed > c.bound {
		t.Fatalf("a blocked warning writer held the stop for %v, past its worst case of %v", elapsed.Round(time.Millisecond), c.bound)
	}

	assertBlockedWriterOutcome(t, c, h, name, warnings, logs)
}

func blockedWriterMarker(c blockedWriterCase, name string) string {
	if c.marker != nil {
		return c.marker(name)
	}
	return name
}

func assertBlockedWriterOutcome(t *testing.T, c blockedWriterCase, h *hub, name string, warnings *gatedWarnings, logs *hookedLog) {
	t.Helper()
	if c.want != nil {
		awaitGatedWrite(t, warnings)
		warnings.open()
		awaitSinkQuiet(t, h.sink)
		want := c.want(h, name)
		if !strings.Contains(warnings.out.String(), want) {
			t.Fatalf("the overrun line never reached the warnings writer once it unblocked: want %q in %q", want, warnings.out.String())
		}
		return
	}
	warnings.open()
	window, cancel := h.newGrace()
	defer cancel()
	if !h.sink.flush(window) {
		t.Fatal("the frozen write did not settle within the grace window")
	}
	h.sink.Close()
	awaitHubLogs(t, h)
	if outside := warnLinesOutsideBilan(h.messages, logs.String()); len(outside) != 0 {
		t.Fatalf("a loss was logged outside the bilan: %q", outside)
	}
	dropped := droppedWarnings(h.sink)
	awaitHubLogs(t, h)
	announced := announcedLostOnOutput(h.messages, warnings.out.String(), dropped) + announcedLostTotal(t, logs.String())
	if dropped < 1 || announced != dropped {
		t.Fatalf("the sink counted %d lost warnings but announced %d on the output %q and in the logs %q, want each lost warning announced exactly once", dropped, announced, warnings.out.String(), notConfirmedLines(logs.String()))
	}
}

func announcedLostOnOutput(messages *hostMessages, out string, dropped int) int {
	total := 0
	for lost := 1; lost <= dropped; lost++ {
		total += lost * strings.Count(out, englishLostLine(messages)(lost)+"\n")
	}
	return total
}

func announcedLostTotal(t *testing.T, logs string) int {
	t.Helper()
	total := 0
	for _, line := range notConfirmedLines(logs) {
		lost, err := strconv.Atoi(logField(line, "lost"))
		if err != nil {
			t.Fatalf("the lost count in %q is not a number: %v", line, err)
		}
		total += lost
	}
	return total
}
