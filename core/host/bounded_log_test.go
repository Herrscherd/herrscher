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

func gateUntilCleanup(t *testing.T) (chan struct{}, func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(open)
	return gate, open
}

func awaitLogIdle(t *testing.T, log *boundedLog) {
	t.Helper()
	if !log.drain(expiresAfter(5 * time.Second)) {
		t.Fatal("the log did not write its pending lines within 5 s")
	}
}

func awaitSinkLog(t *testing.T, sink *warningSink) {
	t.Helper()
	awaitLogIdle(t, &sink.log)
}

func awaitHubLogs(t *testing.T, h *hub) {
	t.Helper()
	if h.sink != nil {
		awaitSinkLog(t, h.sink)
	}
}

func TestBoundedLogWritesTheBilanBeforeThePendingNotes(t *testing.T) {
	frozen := freezeLogOn(t, "blocked line")
	log := boundedLog{logger: slog.Default()}
	log.reserve("blocked", func(logger *slog.Logger) { logger.Warn("blocked line") })
	awaitBackground(t, frozen.entered, "the first line never reached the blocked log")
	log.reserve("queued", func(logger *slog.Logger) { logger.Warn("queued line") })
	log.addBilan(2, 1, writeNotConfirmed)
	frozen.thaw()
	awaitLogIdle(t, &log)
	out := frozen.logs.String()
	bilan := strings.Index(out, "not all confirmed")
	queued := strings.Index(out, "queued line")
	if bilan < 0 || queued < 0 || bilan > queued {
		t.Fatalf("the bilan must be written before the notes pending earlier: bilan at %d, queued line at %d: %q", bilan, queued, out)
	}
}

func TestBoundedLogKeepsItsSummaryStateBounded(t *testing.T) {
	logs := captureDefaultLog(t)
	log := boundedLog{logger: slog.Default()}
	gate, openGate := gateUntilCleanup(t)
	gateEntered := make(chan struct{})
	log.reserve("gate", func(*slog.Logger) { close(gateEntered); <-gate })
	awaitBackground(t, gateEntered, "the gate line never reached the log")
	for range 500 {
		log.addBilan(1, 0, writeNotConfirmed)
		log.addBilan(0, 1, writeNotConfirmed)
		log.reserve("still-running", func(logger *slog.Logger) { logger.Warn("still running line") })
	}
	log.mu.Lock()
	sum, notes := log.bilan, len(log.notes)
	log.mu.Unlock()
	if sum != (bilanSum{lost: 500, unconfirmed: 500}) || notes != 1 {
		t.Fatalf("the pending summaries hold %+v and %d notes, want one sum of 500 and 500 and one note", sum, notes)
	}
	openGate()
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
