package host

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
)

type frozenWriter struct {
	calls     atomic.Int32
	entered   chan struct{}
	release   chan struct{}
	enterOnce sync.Once
	openOnce  sync.Once
	out       lockedBuffer
}

func newFrozenWriter() *frozenWriter {
	return &frozenWriter{entered: make(chan struct{}), release: make(chan struct{})}
}

func (f *frozenWriter) Write(p []byte) (int, error) {
	f.calls.Add(1)
	f.enterOnce.Do(func() {
		close(f.entered)
		<-f.release
	})
	return f.out.Write(p)
}

func (f *frozenWriter) unblock() {
	f.openOnce.Do(func() { close(f.release) })
}

func TestNoWarningWriteStartsAfterStopReturns(t *testing.T) {
	name := "stderr-" + newTurnID()
	writer := newFrozenWriter()
	t.Cleanup(writer.unblock)
	h := newInflightHub(t, name, time.Minute, writer)
	h.warnSessionOverrun(name)
	awaitBackground(t, writer.entered, "the first overrun line never reached the warnings writer")
	h.warnSessionOverrun(name)
	if err := h.closeAndRemoveSockets(); err != nil {
		t.Fatal(err)
	}
	writer.unblock()
	h.closeWarnings()
	h.warnSessionOverrun(name)
	if got := writer.calls.Load(); got != 1 {
		t.Fatalf("the warnings writer was written %d times, want 1: the queued line and the late one must never start a write once Stop returned", got)
	}
}

func TestSessionOverrunGoesThroughTheWarningSink(t *testing.T) {
	var warnings lockedBuffer
	h := testHub(&hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: &warnings, teardownGrace: time.Second, live: make(map[string]liveSession)})
	h.warnSessionOverrun("ordered")
	awaitSinkQuiet(t, h.sink)
	if !strings.Contains(warnings.String(), `"ordered"`) {
		t.Fatalf("the session overrun did not reach the warning sink: %q", warnings.String())
	}
}

func TestSessionOverrunDoesNotNeedTheLifecycleLock(t *testing.T) {
	var warnings lockedBuffer
	h := testHub(&hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: &warnings, teardownGrace: time.Second, live: make(map[string]liveSession)})
	h.lifecycle.Lock()
	done := runInBackground(func() { h.warnSessionOverrun("held") })
	awaitBackground(t, done, "the session overrun waited for the lifecycle lock held by its caller")
	h.lifecycle.Unlock()
	awaitSinkQuiet(t, h.sink)
	if !strings.Contains(warnings.String(), `"held"`) {
		t.Fatalf("the session overrun was not written while the lifecycle was held: %q", warnings.String())
	}
}

func TestSessionOverrunBehindAFrozenSinkIsCountedNotWritten(t *testing.T) {
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	h := testHub(&hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: writer, teardownGrace: time.Second, live: make(map[string]liveSession)})
	if _, err := h.warnings.Write([]byte("frozen\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, writer)
	before := droppedWarnings(h.sink)
	h.warnSessionOverrun("late")
	if got := droppedWarnings(h.sink); got != before+1 {
		t.Fatalf("dropped count went from %d to %d, want the lost overrun counted", before, got)
	}
	writer.open()
	awaitWarning(t, &writer.out, "frozen\n")
	awaitSinkQuiet(t, h.sink)
	if strings.Contains(writer.out.String(), `"late"`) {
		t.Fatalf("an overrun counted as lost reached the output after the writer unblocked: %q", writer.out.String())
	}
}
