package host

import (
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type frozenLog struct {
	logs     *hookedLog
	entered  chan struct{}
	open     chan struct{}
	openOnce sync.Once
}

func freezeLogOn(t *testing.T, marker string) *frozenLog {
	t.Helper()
	frozen := &frozenLog{entered: make(chan struct{}), open: make(chan struct{})}
	var enterOnce sync.Once
	t.Cleanup(frozen.thaw)
	frozen.logs = captureDefaultLog(t, func(line string) {
		if strings.Contains(line, marker) {
			enterOnce.Do(func() { close(frozen.entered) })
			<-frozen.open
		}
	})
	return frozen
}

func (f *frozenLog) thaw() {
	f.openOnce.Do(func() { close(f.open) })
}

func TestBoundedLogAnnouncesEveryAbandonBeforeItStops(t *testing.T) {
	frozen := freezeLogOn(t, "blocked line")
	var log boundedLog
	log.warn(func() { slog.Warn("blocked line") })
	awaitBackground(t, frozen.entered, "the first line never reached the blocked log")
	for range boundedLogQueue {
		log.warn(func() { slog.Warn("queued line") })
	}
	log.warn(func() { slog.Warn("overflow line") })
	frozen.thaw()
	if !log.drain(expiresAfter(5 * time.Second)) {
		t.Fatal("the log did not stop within 5 s of being released")
	}
	out := frozen.logs.String()
	if got := strings.Count(out, "abandoned=1"); got != 1 {
		t.Fatalf("the abandoned line was announced %d times, want exactly once: %q", got, out)
	}
	if got := strings.Count(out, "queued line"); got != boundedLogQueue {
		t.Fatalf("%d queued lines were written, want all %d", got, boundedLogQueue)
	}
	if strings.Contains(out, "overflow line") {
		t.Fatalf("a line refused by the full queue was written: %q", out)
	}
}

func TestBoundedLogWritesItsSummariesWhenItAdvances(t *testing.T) {
	frozen := freezeLogOn(t, "blocked line")
	var log boundedLog
	log.warn(func() { slog.Warn("blocked line") })
	awaitBackground(t, frozen.entered, "the first line never reached the blocked log")
	for range boundedLogQueue {
		log.warn(func() { slog.Warn("queued line") })
	}
	log.reserve(func() { slog.Warn("warnings were not all confirmed when the sink closed", "lost", 2, "unconfirmed", 0) })
	log.reserve(func() { slog.Warn("warnings were not all confirmed when the sink closed", "lost", 3, "unconfirmed", 0) })
	if log.drain(expiresAfter(50 * time.Millisecond)) {
		t.Fatal("the log reported idle while its writer was still blocked")
	}
	frozen.thaw()
	if !log.drain(expiresAfter(5 * time.Second)) {
		t.Fatal("the log did not stop within 5 s of being released")
	}
	out := frozen.logs.String()
	for _, want := range []string{"lost=2 unconfirmed=0", "lost=3 unconfirmed=0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the summary %q was replaced or dropped while the queue was full: %q", want, out)
		}
	}
}
