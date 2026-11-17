package host

import (
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestBoundedLogKeepsOneWriterAndCountsTheRest(t *testing.T) {
	const marker = "first line of a blocked log"
	const extra = 4
	entered := make(chan struct{})
	release := make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	logs := captureDefaultLog(t, func(line string) {
		if strings.Contains(line, marker) {
			enterOnce.Do(func() { close(entered) })
			<-release
		}
	})
	var log boundedLog
	log.warn(func() { slog.Warn(marker) })
	awaitBackground(t, entered, "the first line never reached the blocked log")
	for range extra {
		log.warn(func() { slog.Warn("a line that must not start a writer") })
	}
	if got := log.abandoned.Load(); got != extra {
		t.Fatalf("abandoned = %d, want %d: each line offered while one writer is stuck must be counted and dropped, not spawn a goroutine", got, extra)
	}
	releaseOnce.Do(func() { close(release) })
	waitUntil(t, "the abandoned count to be announced", func() bool {
		return strings.Contains(logs.String(), "abandoned="+strconv.Itoa(extra))
	})
	waitUntil(t, "the writer to release the log", func() bool { return !log.busy.Load() })
	if strings.Contains(logs.String(), "a line that must not start a writer") {
		t.Fatalf("a line offered while the log was blocked was written: %q", logs.String())
	}
}
