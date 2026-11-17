package host

import (
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type frozenStderr struct {
	calls     atomic.Int32
	entered   chan struct{}
	release   chan struct{}
	enterOnce sync.Once
	openOnce  sync.Once
	out       lockedBuffer
}

func newFrozenStderr() *frozenStderr {
	return &frozenStderr{entered: make(chan struct{}), release: make(chan struct{})}
}

func (f *frozenStderr) Write(p []byte) (int, error) {
	f.calls.Add(1)
	f.enterOnce.Do(func() {
		close(f.entered)
		<-f.release
	})
	return f.out.Write(p)
}

func (f *frozenStderr) unblock() {
	f.openOnce.Do(func() { close(f.release) })
}

func TestNoStderrWriteStartsAfterStopReturns(t *testing.T) {
	name := "stderr-" + newTurnID()
	writer := newFrozenStderr()
	t.Cleanup(writer.unblock)
	h := newInflightHub(t, name, time.Minute, io.Discard, withStderr(writer))
	h.warnSessionOverrun(name)
	awaitBackground(t, writer.entered, "the first overrun line never reached stderr")
	h.warnSessionOverrun(name)
	if err := h.closeAndRemoveSockets(); err != nil {
		t.Fatal(err)
	}
	writer.unblock()
	if !h.errPort.shutdown(expiresAfter(5 * time.Second)) {
		t.Fatal("the stderr writer did not stop within 5 s of being released")
	}
	h.warnSessionOverrun(name)
	if got := writer.calls.Load(); got != 1 {
		t.Fatalf("stderr was written %d times, want 1: the queued line and the late one must never start a write once Stop returned", got)
	}
}

func TestCloseAndRemoveSocketsHaltsTheStderrPortWithoutAShutdown(t *testing.T) {
	name := "stderr-" + newTurnID()
	var out lockedBuffer
	h := newInflightHub(t, name, time.Minute, io.Discard, withStderr(&out))
	h.warnSessionOverrun(name)
	if err := h.closeAndRemoveSockets(); err != nil {
		t.Fatal(err)
	}
	if !h.errPort.isHalted() {
		t.Fatal("closeAndRemoveSockets returned with the stderr port open: nothing else halts it, so later overrun lines would be written after the hub stopped")
	}
}

func TestCloseStderrLogsWhatItAbandons(t *testing.T) {
	t.Run("a write left behind is logged as not joined", func(t *testing.T) {
		writer := newFrozenStderr()
		t.Cleanup(writer.unblock)
		h := testHub(&hub{warnings: io.Discard, stderr: writer})
		_ = h.errPort.offer(writer, "held")
		awaitBackground(t, writer.entered, "the line never reached the blocked writer")
		logs := captureDefaultLog(t)
		h.closeStderr()
		if !strings.Contains(logs.String(), "writer_joined=false") {
			t.Fatalf("closeStderr did not log the write it left behind: %q", logs.String())
		}
	})
	t.Run("lines dropped after the port halted are logged with their count", func(t *testing.T) {
		var out lockedBuffer
		h := testHub(&hub{warnings: io.Discard, stderr: &out})
		h.errPort.shutdown(expiresAfter(time.Second))
		_ = h.errPort.offer(&out, "late")
		logs := captureDefaultLog(t)
		h.closeStderr()
		if !strings.Contains(logs.String(), "dropped=1") || !strings.Contains(logs.String(), "writer_joined=true") {
			t.Fatalf("closeStderr logged %q, want the one dropped line and a joined writer", logs.String())
		}
	})
}
