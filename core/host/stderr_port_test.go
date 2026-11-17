package host

import (
	"io"
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
