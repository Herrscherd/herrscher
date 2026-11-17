package host

import (
	"log/slog"
	"strconv"
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

func awaitLogIdle(t *testing.T, log *boundedLog) {
	t.Helper()
	if !log.drain(expiresAfter(5 * time.Second)) {
		t.Fatal("the log did not write its pending lines within 5 s")
	}
}

func announcedAbandons(out string) int {
	total := 0
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "warning lines were abandoned") {
			continue
		}
		count, err := strconv.Atoi(logField(line, "abandoned"))
		if err == nil {
			total += count
		}
	}
	return total
}

func TestBoundedLogHoldsItsLinesWhileTheWriterIsFrozen(t *testing.T) {
	frozen := freezeLogOn(t, "blocked line")
	var log boundedLog
	log.warn(func() { slog.Warn("blocked line") })
	awaitBackground(t, frozen.entered, "the first line never reached the blocked log")
	for range boundedLogQueue {
		log.warn(func() { slog.Warn("queued line") })
	}
	log.warn(func() { slog.Warn("overflow line") })
	if strings.Contains(frozen.logs.String(), "queued line") {
		t.Fatal("a queued line was written while the writer was still blocked")
	}
	frozen.thaw()
	awaitLogIdle(t, &log)
	out := frozen.logs.String()
	if got := announcedAbandons(out); got != 1 {
		t.Fatalf("the abandoned lines were announced %d times in total, want exactly one line of 1: %q", got, out)
	}
	if got := strings.Count(out, "queued line"); got != boundedLogQueue {
		t.Fatalf("%d queued lines were written, want all %d", got, boundedLogQueue)
	}
	if strings.Contains(out, "overflow line") {
		t.Fatalf("a line refused by the full queue was written: %q", out)
	}
}

func TestBoundedLogCountsEveryConcurrentAbandon(t *testing.T) {
	frozen := freezeLogOn(t, "blocked line")
	var log boundedLog
	log.warn(func() { slog.Warn("blocked line") })
	awaitBackground(t, frozen.entered, "the first line never reached the blocked log")
	const writers, linesEach = 8, 25
	var offering sync.WaitGroup
	for range writers {
		offering.Add(1)
		go func() {
			defer offering.Done()
			for range linesEach {
				log.warn(func() { slog.Warn("concurrent line") })
			}
		}()
	}
	offering.Wait()
	frozen.thaw()
	awaitLogIdle(t, &log)
	out := frozen.logs.String()
	if got := strings.Count(out, "concurrent line"); got != boundedLogQueue {
		t.Fatalf("%d concurrent lines were written, want the %d that fitted the queue", got, boundedLogQueue)
	}
	if got, want := announcedAbandons(out), writers*linesEach-boundedLogQueue; got != want {
		t.Fatalf("the announcements count %d abandoned lines, want %d: an abandon was lost or counted twice: %q", got, want, out)
	}
}

func TestBoundedLogAnnouncesAbandonsOfferedWhileTheAnnouncementIsFrozen(t *testing.T) {
	frozen := freezeLogOn(t, "abandoned=")
	var log boundedLog
	gate := make(chan struct{})
	log.warn(func() { <-gate })
	for range boundedLogQueue {
		log.warn(func() { slog.Warn("first line") })
	}
	log.warn(func() { slog.Warn("first refused") })
	close(gate)
	awaitBackground(t, frozen.entered, "the abandon announcement never reached the frozen log")
	for range boundedLogQueue {
		log.warn(func() { slog.Warn("second line") })
	}
	refusing := runInBackground(func() {
		for range 3 {
			log.warn(func() { slog.Warn("second refused") })
		}
	})
	awaitBackground(t, refusing, "a refused line blocked the caller while the announcement was frozen")
	if strings.Contains(frozen.logs.String(), "second line") {
		t.Fatal("a second writer wrote a queued line while the first one was blocked in the announcement")
	}
	frozen.thaw()
	awaitLogIdle(t, &log)
	out := frozen.logs.String()
	if got := strings.Count(out, "second line"); got != boundedLogQueue {
		t.Fatalf("%d second lines were written, want all %d", got, boundedLogQueue)
	}
	if got := announcedAbandons(out); got != 4 {
		t.Fatalf("the announcements count %d abandoned lines, want 4: the abandons offered during the frozen announcement were lost: %q", got, out)
	}
}

func TestBoundedLogWritesTheBilanBeforeTheQueuedLines(t *testing.T) {
	frozen := freezeLogOn(t, "blocked line")
	var log boundedLog
	log.warn(func() { slog.Warn("blocked line") })
	awaitBackground(t, frozen.entered, "the first line never reached the blocked log")
	log.warn(func() { slog.Warn("queued line") })
	log.addBilan(2, 1, writeNotConfirmed)
	frozen.thaw()
	awaitLogIdle(t, &log)
	out := frozen.logs.String()
	bilan := strings.Index(out, "not all confirmed")
	queued := strings.Index(out, "queued line")
	if bilan < 0 || queued < 0 || bilan > queued {
		t.Fatalf("the bilan must be written before the lines queued earlier: bilan at %d, queued line at %d: %q", bilan, queued, out)
	}
}

func TestBoundedLogKeepsItsSummaryStateBounded(t *testing.T) {
	logs := captureDefaultLog(t)
	var log boundedLog
	gate := make(chan struct{})
	log.warn(func() { <-gate })
	for range 500 {
		log.addBilan(1, 0, writeNotConfirmed)
		log.addBilan(0, 1, writeNotConfirmed)
		log.reserve("still-running", func() { slog.Warn("still running line") })
	}
	log.mu.Lock()
	sum, notes := log.bilan, len(log.notes)
	log.mu.Unlock()
	if sum != (bilanSum{lost: 500, unconfirmed: 500}) || notes != 1 {
		t.Fatalf("the pending summaries hold %+v and %d notes, want one sum of 500 and 500 and one note", sum, notes)
	}
	close(gate)
	awaitLogIdle(t, &log)
	out := logs.String()
	if got := strings.Count(out, "not all confirmed"); got != 1 {
		t.Fatalf("%d bilan lines were written, want the 1000 deltas in one line: %q", got, out)
	}
	if got := strings.Count(out, "still running line"); got != 1 {
		t.Fatalf("%d notes were written for one key, want 1: %q", got, out)
	}
	line := notConfirmedLines(out)[0]
	if logField(line, "lost") != "500" || logField(line, "unconfirmed") != "500" {
		t.Fatalf("the bilan line is %q, want lost=500 unconfirmed=500", line)
	}
}
