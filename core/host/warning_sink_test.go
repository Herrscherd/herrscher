package host

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

type blockedForever struct {
	release chan struct{}
}

func (b *blockedForever) Write(p []byte) (int, error) {
	<-b.release
	return len(p), nil
}

func englishLostLine(lost int) string {
	return newHostMessages().text(i18n.English, "warn.warnings_dropped", map[string]any{countParam: lost})
}

func testHub(h *hub) *hub {
	out := h.warnings
	if out == nil {
		out = io.Discard
	}
	h.sink = newWarningSink(out, englishLostLine)
	h.warnings = h.sink
	return h
}

func droppedWarnings(s *warningSink) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

func queuedWarnings(s *warningSink) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

func TestWarningSinkDoesNotBlockWritersAndCountsDrops(t *testing.T) {
	writer := &blockedForever{release: make(chan struct{})}
	t.Cleanup(func() { close(writer.release) })
	sink := newWarningSink(writer, englishLostLine)

	const writes = 1000
	returned := runInBackground(func() {
		for range writes {
			if _, err := sink.Write([]byte("warning\n")); !isWarningOutcome(err) {
				t.Error(err)
			}
		}
	})
	awaitBackground(t, returned, "writes blocked behind a stuck writer")

	if dropped := droppedWarnings(sink); dropped < writes-warningQueueLimit-1 {
		t.Fatalf("dropped %d of %d writes behind a stuck writer, want at least %d", dropped, writes, writes-warningQueueLimit-1)
	}
}

func TestWarningSinkReportsHowManyWarningsItLostWhenTheWriterUnblocks(t *testing.T) {
	for _, c := range []struct {
		name string
		lang i18n.Language
	}{
		{name: "english", lang: i18n.English},
		{name: "french", lang: i18n.French},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertLostSummaryFollowsUnblock(t, c.lang)
		})
	}
}

func assertLostSummaryFollowsUnblock(t *testing.T, lang i18n.Language) {
	t.Helper()
	writer := newGatedWarnings("blocked")
	sink := newWarningSink(writer, func(lost int) string {
		return newHostMessages().text(lang, "warn.warnings_dropped", map[string]any{countParam: lost})
	})
	if _, err := sink.Write([]byte("blocked\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, writer)
	fillQueueBehindBlockedWrite(t, sink)
	const lost = 10
	writeOverflow(t, sink, lost)
	if got := droppedWarnings(sink); got != lost {
		t.Fatalf("dropped %d warnings behind a full queue, want %d", got, lost)
	}

	writer.open()
	h := testHub(&hub{teardownGrace: 10 * time.Second})
	window, cancel := h.newGrace()
	defer cancel()
	if !sink.flush(window) {
		t.Fatal("the sink did not drain once the writer unblocked")
	}
	want := newHostMessages().text(lang, "warn.warnings_dropped", map[string]any{countParam: lost}) + "\n"
	if !strings.HasPrefix(writer.out.String(), "blocked\n"+want) {
		t.Fatalf("the summary of the %d lost warnings does not follow the write that unblocked:\n%s", lost, writer.out.String())
	}
	if strings.Contains(writer.out.String(), "overflow") {
		t.Fatal("a dropped warning reached the output")
	}
}

func TestLostSummaryUsesTheSingularForOneWarning(t *testing.T) {
	for _, c := range []struct {
		name string
		lang i18n.Language
		lost int
		want string
	}{
		{name: "english one", lang: i18n.English, lost: 1, want: "herrscher serve: 1 warning lost"},
		{name: "english many", lang: i18n.English, lost: 2, want: "herrscher serve: 2 warnings lost"},
		{name: "french one", lang: i18n.French, lost: 1, want: "herrscher serve : 1 avertissement perdu"},
		{name: "french many", lang: i18n.French, lost: 2, want: "herrscher serve : 2 avertissements perdus"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := newHostMessages().text(c.lang, "warn.warnings_dropped", map[string]any{countParam: c.lost})
			if !strings.HasPrefix(got, c.want) {
				t.Fatalf("summary of %d lost warnings = %q, want it to start with %q", c.lost, got, c.want)
			}
		})
	}
}

func fillQueueBehindBlockedWrite(t *testing.T, sink *warningSink) {
	t.Helper()
	for i := range warningQueueLimit {
		if _, err := sink.Write([]byte("item " + strconv.Itoa(i) + "\n")); !errors.Is(err, errWarningLate) {
			t.Fatalf("write returned %v, want errWarningLate", err)
		}
	}
}

func writeOverflow(t *testing.T, sink *warningSink, count int) {
	t.Helper()
	for range count {
		if _, err := sink.Write([]byte("overflow\n")); !errors.Is(err, errWarningAbandoned) {
			t.Fatalf("write returned %v, want errWarningAbandoned", err)
		}
	}
}

func TestWarningSinkWritesAfterCloseReachTheOutput(t *testing.T) {
	var out lockedBuffer
	sink := newWarningSink(&out, englishLostLine)
	if _, err := sink.Write([]byte("before\n")); err != nil {
		t.Fatal(err)
	}
	sink.Close()
	if _, err := sink.Write([]byte("after\n")); err != nil {
		t.Fatalf("a write after Close returned %v, want it written", err)
	}
	if got := out.String(); got != "before\nafter\n" {
		t.Fatalf("output after Close = %q, want the write before it then the write after it", got)
	}
}

func TestWarningSinkAfterCloseIsBoundedByAStalledWriter(t *testing.T) {
	writer := newGatedWarnings("stuck")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	sink.Close()

	started := time.Now()
	if _, err := sink.Write([]byte("stuck\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("a write behind a stalled writer after Close returned %v, want errWarningLate", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("a write after Close held %v behind a stalled writer, want under 1s", elapsed.Round(time.Millisecond))
	}
}

func TestWarningSinkFlushIsBoundedByAStalledWriter(t *testing.T) {
	writer := newGatedWarnings("stuck")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("stuck\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	h := testHub(&hub{teardownGrace: 5 * time.Second})
	window, cancel := h.newGrace()
	defer cancel()

	started := time.Now()
	if sink.flush(window) {
		t.Fatal("flush reported drained behind a stalled writer")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("flush held %v behind a stalled writer with a 5s grace, want under 1s", elapsed.Round(time.Millisecond))
	}
}

func TestWarningSinkCountsAnOversizedEntryAsLost(t *testing.T) {
	var out lockedBuffer
	sink := newWarningSink(&out, englishLostLine)
	oversized := bytes.Repeat([]byte("x"), warningEntryBytes+1)
	if _, err := sink.Write(oversized); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("write returned %v, want errWarningAbandoned", err)
	}
	if got := droppedWarnings(sink); got != 1 {
		t.Fatalf("dropped %d entries for one oversized warning, want 1", got)
	}
	if _, err := sink.Write([]byte("after\n")); err != nil {
		t.Fatal(err)
	}
	if want := englishLostLine(1) + "\nafter\n"; out.String() != want {
		t.Fatalf("output = %q, want the summary of the lost warning then the next one %q", out.String(), want)
	}
}

func TestWarningSinkCountsAnEntryOverTheByteBudgetAsLost(t *testing.T) {
	writer := newGatedWarnings("blocked")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("blocked\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, writer)
	entry := bytes.Repeat([]byte("y"), warningEntryBytes)
	for range warningQueueBytes / warningEntryBytes {
		if _, err := sink.Write(entry); !errors.Is(err, errWarningLate) {
			t.Fatalf("write returned %v, want errWarningLate", err)
		}
	}
	if got := droppedWarnings(sink); got != 0 {
		t.Fatalf("dropped %d entries within the byte budget, want 0", got)
	}
	if _, err := sink.Write(entry); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("write returned %v, want errWarningAbandoned", err)
	}
	if got := droppedWarnings(sink); got != 1 {
		t.Fatalf("dropped %d entries past the byte budget, want 1", got)
	}
}

func TestLateWritesBehindAFrozenWriterAreLostWithoutAGoroutine(t *testing.T) {
	writer := newGatedWarnings("stuck")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("stuck\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, writer)
	sink.Close()

	before := runtime.NumGoroutine()
	for range 20 {
		if _, err := sink.Write([]byte("stuck late\n")); !errors.Is(err, errWarningAbandoned) {
			t.Fatalf("a late write behind a frozen writer returned %v, want errWarningAbandoned", err)
		}
	}
	if grown := runtime.NumGoroutine() - before; grown > 0 {
		t.Fatalf("20 late writes that never got the writer added %d goroutines, want none", grown)
	}
	if dropped := droppedWarnings(sink); dropped != 20 {
		t.Fatalf("%d late writes were abandoned without being counted as lost, want 20", dropped)
	}
}

func TestSinkSerializesDrainAndDirectWritesOnABareBuffer(t *testing.T) {
	const writes = 300
	var out bytes.Buffer
	h := testHub(&hub{warnings: &out, teardownGrace: 10 * time.Second})
	direct := runInBackground(func() {
		for range writes {
			if err := h.sink.announce("direct"); err != nil {
				t.Error(err)
			}
		}
	})
	for range writes {
		if _, err := h.sink.Write([]byte("queued\n")); !isWarningOutcome(err) {
			t.Error(err)
		}
	}
	<-direct
	window, cancel := h.newGrace()
	defer cancel()
	if !h.sink.flush(window) {
		t.Fatal("the sink did not drain")
	}
	if got := strings.Count(out.String(), "direct\n"); got != writes {
		t.Fatalf("%d direct writes reached the output, want %d", got, writes)
	}
}

func TestOverrunWaitsForAPendingDirectWriteInsteadOfBeingRefused(t *testing.T) {
	writer := newGatedWarnings("late")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	sink.Close()

	if _, err := sink.Write([]byte("late\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("a late write behind a frozen writer returned %v, want errWarningLate", err)
	}
	time.AfterFunc(50*time.Millisecond, writer.open)
	if err := sink.announce("overrun"); err != nil {
		t.Fatalf("the overrun was refused behind a direct write that was then released: %v", err)
	}
	if !strings.Contains(writer.out.String(), "overrun\n") {
		t.Fatalf("the overrun did not reach the output: %q", writer.out.String())
	}
	if _, err := sink.Write([]byte("after\n")); err != nil {
		t.Fatalf("a write after the release returned %v, want nil", err)
	}
	if !strings.Contains(writer.out.String(), "after\n") {
		t.Fatalf("the write after the release did not reach the output: %q", writer.out.String())
	}
}

func TestOverrunIsCountedWhenTheWriterStaysFrozenForTheWholeBudget(t *testing.T) {
	writer := newGatedWarnings("late")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	sink.Close()
	if _, err := sink.Write([]byte("late\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("a late write behind a frozen writer returned %v, want errWarningLate", err)
	}
	before := droppedWarnings(sink)
	if err := sink.announce("overrun"); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("an overrun behind a frozen writer returned %v, want errWarningAbandoned", err)
	}
	if got := droppedWarnings(sink); got != before+1 {
		t.Fatalf("dropped count went from %d to %d, want the lost overrun counted", before, got)
	}
	writer.open()
	awaitWarning(t, &writer.out, "late\n")
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(writer.out.String(), "overrun") {
		t.Fatalf("an overrun counted as lost reached the output after the writer unblocked: %q", writer.out.String())
	}
}

func TestDirectWriteBehindAStalledSinkNeverWaitsAndIsLostNeverWritten(t *testing.T) {
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("frozen\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, writer)

	started := time.Now()
	if err := sink.announce("FIRST"); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("the first announce behind a stalled sink returned %v, want errWarningAbandoned", err)
	}
	if err := sink.announce("SECOND"); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("the second announce behind a stalled sink returned %v, want errWarningAbandoned", err)
	}
	if elapsed := time.Since(started); elapsed > warningWriteBudget/2 {
		t.Fatalf("two announces behind a stalled sink held %v, want under %v", elapsed.Round(time.Millisecond), warningWriteBudget/2)
	}
	if got := droppedWarnings(sink); got != 2 {
		t.Fatalf("dropped %d warnings, want 2: both announces never obtained the writer", got)
	}
	writer.open()
	awaitWarning(t, &writer.out, "frozen\n")
	time.Sleep(100 * time.Millisecond)
	if out := writer.out.String(); strings.Contains(out, "FIRST") || strings.Contains(out, "SECOND") {
		t.Fatalf("an announce counted as lost reached the output: %q", out)
	}
}

func TestLateWriteKeepsTheBytesItWasGiven(t *testing.T) {
	writer := newGatedWarnings("LATEMSG")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	sink.Close()
	buf := []byte("LATEMSG\n")
	if _, err := sink.Write(buf); !errors.Is(err, errWarningLate) {
		t.Fatalf("a late write behind a frozen writer returned %v, want errWarningLate", err)
	}
	buf[0] = 'X'
	writer.open()
	awaitWarning(t, &writer.out, "LATEMSG\n")
	if strings.Contains(writer.out.String(), "XATEMSG") {
		t.Fatalf("the late write reached the output with the caller's later edit: %q", writer.out.String())
	}
	if dropped := droppedWarnings(sink); dropped != 0 {
		t.Fatalf("an unconfirmed write was counted as lost: dropped %d, want 0", dropped)
	}
}

func TestCloseReportsTheFinalCountWhenItsSummaryCannotBeWritten(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("frozen\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, writer)
	if err := sink.announce("HOLD"); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("the announce behind the frozen writer returned %v, want errWarningAbandoned", err)
	}
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("write returned %v, want errWarningAbandoned", err)
	}
	sink.Close()
	line := unconfirmedLine(logs.String())
	if logField(line, "lost") != "2" || logField(line, "unconfirmed") != "1" {
		t.Fatalf("Close logged %q, want lost=2 and unconfirmed=1: the announce and the oversized warning are lost, the frozen write is unconfirmed", line)
	}
}

func TestFailedSummaryStaysUnreportedUntilItIsWritten(t *testing.T) {
	writer := &summaryRefusingWriter{}
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("write returned %v, want errWarningAbandoned", err)
	}
	if _, err := sink.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Write([]byte("second\n")); err != nil {
		t.Fatal(err)
	}
	want := "first\n" + englishLostLine(1) + "\nsecond\n"
	if got := writer.out.String(); got != want {
		t.Fatalf("output = %q, want %q: a refused summary is written again before the next warning", got, want)
	}
}

type summaryRefusingWriter struct {
	out     lockedBuffer
	refused bool
}

func (w *summaryRefusingWriter) Write(p []byte) (int, error) {
	if !w.refused && strings.Contains(string(p), "lost") {
		w.refused = true
		return 0, errors.New("the summary was refused")
	}
	return w.out.Write(p)
}

func captureDefaultLog(t *testing.T) *lockedBuffer {
	t.Helper()
	logs := &lockedBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return logs
}

func unconfirmedLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "not all confirmed") {
			return line
		}
	}
	return ""
}

func TestOverrunBehindAFrozenDrainIsLostOrUnconfirmedAtClose(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("frozen\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, writer)
	before := droppedWarnings(sink)
	if err := sink.announce("OVERRUN"); !errors.Is(err, errWarningAbandoned) && !errors.Is(err, errWarningLate) {
		t.Fatalf("the overrun behind a frozen drain returned %v", err)
	}
	sink.Close()
	lost := droppedWarnings(sink) - before
	unconfirmed := unconfirmedAtClose(t, logs.String())
	if lost == 0 && unconfirmed == 0 {
		t.Fatal("the overrun behind a frozen drain was neither counted lost nor reported unconfirmed at Close")
	}
	if lost > 0 && strings.Contains(writer.out.String(), "OVERRUN") {
		t.Fatalf("the overrun was counted lost and written: %q", writer.out.String())
	}
}

func TestSummaryInFlightAtCloseIsReportedAsLostAndUnconfirmed(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("lost")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("write returned %v, want errWarningAbandoned", err)
	}
	if _, err := sink.Write([]byte("A\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, writer)
	sink.Close()
	line := unconfirmedLine(logs.String())
	if logField(line, "lost") != "1" || unconfirmedAtClose(t, logs.String()) < 1 {
		t.Fatalf("Close logged %q, want lost=1 and unconfirmed>=1 while the summary is in flight", line)
	}
}

func TestCloseNamesAnUnconfirmedWriteEvenWhenNothingWasLost(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("frozen\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, writer)
	sink.Close()
	line := unconfirmedLine(logs.String())
	if logField(line, "lost") != "0" || logField(line, "unconfirmed") != "1" {
		t.Fatalf("Close logged %q, want lost=0 and unconfirmed=1: the frozen write is in flight and nothing was dropped", line)
	}
}

func TestQueuedBehindAFrozenWriterIsLostAtCloseAndNeverWritten(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("frozen\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, writer)
	if _, err := sink.Write([]byte("QUEUEDMSG\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	sink.Close()
	if got := droppedWarnings(sink); got != 1 {
		t.Fatalf("dropped %d warnings at Close, want 1: the queued warning was never handed to the writer", got)
	}
	line := unconfirmedLine(logs.String())
	if logField(line, "lost") != "1" || logField(line, "unconfirmed") != "1" {
		t.Fatalf("Close logged %q, want lost=1 and unconfirmed=1", line)
	}
	writer.open()
	awaitWarning(t, &writer.out, "frozen\n")
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(writer.out.String(), "QUEUEDMSG") {
		t.Fatalf("a warning counted as lost reached the output: %q", writer.out.String())
	}
}

func unconfirmedAtClose(t *testing.T, logs string) int {
	t.Helper()
	line := unconfirmedLine(logs)
	_, value, found := strings.Cut(line, "unconfirmed=")
	if !found {
		return 0
	}
	count, err := strconv.Atoi(strings.Fields(value)[0])
	if err != nil {
		t.Fatalf("the unconfirmed count in %q is not a number: %v", line, err)
	}
	return count
}

func isWarningOutcome(err error) bool {
	return err == nil || errors.Is(err, errWarningLate) || errors.Is(err, errWarningAbandoned)
}

func unconfirmedCount(s *warningSink) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unconfirmed
}

func waitUntil(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func lastUnconfirmedLine(text string) string {
	last := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "not all confirmed") {
			last = line
		}
	}
	return last
}

type refusingEntryWriter struct {
	marker string
	out    lockedBuffer
}

var errEntryRefused = errors.New("the writer refused the entry")

func (w *refusingEntryWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.marker) {
		return 0, errEntryRefused
	}
	return w.out.Write(p)
}

func TestQueuedWriterErrorIsCountedLostBeforeTheNextWrite(t *testing.T) {
	writer := &refusingEntryWriter{marker: "REFUSED"}
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("REFUSED\n")); !errors.Is(err, errEntryRefused) {
		t.Fatalf("the refused queued warning returned %v, want the writer's error", err)
	}
	if _, err := sink.Write([]byte("after\n")); err != nil {
		t.Fatalf("the write after the refused one returned %v, want nil", err)
	}
	sink.Close()
	if want := englishLostLine(1) + "\nafter\n"; writer.out.String() != want {
		t.Fatalf("output = %q, want %q: the refused queued warning must be counted lost and announced before the next write", writer.out.String(), want)
	}
}

type blockedRefusingWriter struct {
	entered   chan struct{}
	release   chan struct{}
	enterOnce sync.Once
	openOnce  sync.Once
}

func (w *blockedRefusingWriter) Write(_ []byte) (int, error) {
	w.enterOnce.Do(func() { close(w.entered) })
	<-w.release
	return 0, errEntryRefused
}

func (w *blockedRefusingWriter) unblock() {
	w.openOnce.Do(func() { close(w.release) })
}

func awaitBackground(t *testing.T, done <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal(failure)
	}
}

func TestWriteFailureAfterCloseIsAnnounced(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := &blockedRefusingWriter{entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(writer.unblock)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("INFLIGHT\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitBackground(t, writer.entered, "the warning was never handed to the writer")
	sink.Close()
	writer.unblock()
	waitUntil(t, "the failed in-flight write to be announced", func() bool { return logField(lastUnconfirmedLine(logs.String()), "lost") == "1" })
	line := lastUnconfirmedLine(logs.String())
	if logField(line, "lost") != "1" || logField(line, "unconfirmed") != "0" {
		t.Fatalf("the in-flight write failing after Close logged %q, want lost=1 and unconfirmed=0", line)
	}
}

type firstWriteRefused struct {
	entered  chan struct{}
	release  chan struct{}
	started  atomic.Bool
	out      lockedBuffer
	openOnce sync.Once
}

func newFirstWriteRefused() *firstWriteRefused {
	return &firstWriteRefused{entered: make(chan struct{}), release: make(chan struct{})}
}

func (w *firstWriteRefused) Write(p []byte) (int, error) {
	if !w.started.Swap(true) {
		close(w.entered)
		<-w.release
		return 0, errEntryRefused
	}
	return w.out.Write(p)
}

func (w *firstWriteRefused) unblock() {
	w.openOnce.Do(func() { close(w.release) })
}

func isSummarizing(s *warningSink) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summarizing
}

func notConfirmedLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "not all confirmed") {
			lines = append(lines, line)
		}
	}
	return lines
}

func logField(line, key string) string {
	for _, field := range strings.Fields(line) {
		if value, found := strings.CutPrefix(field, key+"="); found {
			return value
		}
	}
	return ""
}

func TestWriterErrorDuringCloseIsAnnouncedOnce(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newFirstWriteRefused()
	t.Cleanup(writer.unblock)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("INFLIGHT\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitBackground(t, writer.entered, "the warning was never handed to the writer")
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("oversized write returned %v, want errWarningAbandoned", err)
	}
	closed := runInBackground(sink.Close)
	waitUntil(t, "Close to wait for the summary behind the in-flight write", func() bool { return isSummarizing(sink) })
	writer.unblock()
	awaitBackground(t, closed, "the background Close did not return")
	lines := notConfirmedLines(logs.String())
	if len(lines) != 1 {
		t.Fatalf("closing logged %d unconfirmed lines %q, want exactly one", len(lines), lines)
	}
	if lost, unconfirmed := logField(lines[0], "lost"), logField(lines[0], "unconfirmed"); lost != "1" || unconfirmed != "0" {
		t.Fatalf("the failed in-flight write during Close logged lost=%s unconfirmed=%s, want lost=1 unconfirmed=0: %q", lost, unconfirmed, lines[0])
	}
}

func lineContaining(text, marker string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	return ""
}

func TestSummaryFailureAfterCloseIsAnnounced(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newFirstWriteRefused()
	t.Cleanup(writer.unblock)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("oversized write returned %v, want errWarningAbandoned", err)
	}
	sink.Close()
	writer.unblock()
	waitUntil(t, "the summary that failed after Close to be announced", func() bool {
		return strings.Contains(logs.String(), "could not be written")
	})
	bilan := notConfirmedLines(logs.String())
	if len(bilan) != 1 || logField(bilan[0], "lost") != "1" || logField(bilan[0], "unconfirmed") != "1" {
		t.Fatalf("Close logged %q, want one bilan with lost=1 unconfirmed=1 while the summary is in flight", bilan)
	}
	failure := lineContaining(logs.String(), "could not be written")
	if logField(failure, "lost") != "1" {
		t.Fatalf("the summary failure after Close logged %q, want lost=1", failure)
	}
}

func TestSecondCloseIsSilentAndRecountsNothing(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("frozen\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	if _, err := sink.Write([]byte("QUEUED\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	sink.Close()
	droppedAfterFirst := droppedWarnings(sink)
	logsAfterFirst := strings.Count(logs.String(), "not all confirmed")
	if logsAfterFirst != 1 {
		t.Fatalf("the first Close logged %d unconfirmed lines, want 1", logsAfterFirst)
	}

	sink.Close()
	if got := droppedWarnings(sink); got != droppedAfterFirst {
		t.Fatalf("a second Close changed dropped from %d to %d, want no recount", droppedAfterFirst, got)
	}
	if got := strings.Count(logs.String(), "not all confirmed"); got != logsAfterFirst {
		t.Fatalf("a second Close logged %d more lines, want none", got-logsAfterFirst)
	}
}

func TestWarningAfterCloseIsAnnouncedWithItsLostAndUnconfirmedCounts(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("frozen\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	sink.Close()
	before := strings.Count(logs.String(), "not all confirmed")

	if err := sink.announce("OVERRUN"); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("the overrun behind a frozen writer after Close returned %v, want errWarningAbandoned", err)
	}
	if got := strings.Count(logs.String(), "not all confirmed"); got != before+1 {
		t.Fatalf("the lost overrun after Close logged %d new lines, want 1", got-before)
	}
	if line := lastUnconfirmedLine(logs.String()); logField(line, "lost") != "1" || logField(line, "unconfirmed") != "1" {
		t.Fatalf("the lost overrun after Close logged %q, want lost=1 and unconfirmed=1", line)
	}
}

func TestLateSuccessAfterAnUnconfirmedWriteIsWrittenOnceAndNotCounted(t *testing.T) {
	writer := newGatedWarnings("LATE")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	sink.Close()
	if _, err := sink.Write([]byte("LATE\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("a late write behind a frozen writer returned %v, want errWarningLate", err)
	}
	if got := unconfirmedCount(sink); got != 1 {
		t.Fatalf("unconfirmed = %d while the writer is frozen, want 1", got)
	}

	writer.open()
	waitUntil(t, "the late write to settle", func() bool { return unconfirmedCount(sink) == 0 })
	if got := strings.Count(writer.out.String(), "LATE\n"); got != 1 {
		t.Fatalf("the late warning was written %d times, want once: %q", got, writer.out.String())
	}
	if got := droppedWarnings(sink); got != 0 {
		t.Fatalf("dropped = %d after a late success, want 0: a confirmed write is not lost", got)
	}
}

func TestQueuedWarningLostAtCloseReturnsAbandonedAndWritesNothing(t *testing.T) {
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	frozen := runInBackground(func() {
		if _, err := sink.Write([]byte("frozen\n")); !errors.Is(err, errWarningLate) {
			t.Errorf("write returned %v, want errWarningLate", err)
		}
	})
	awaitGatedWrite(t, writer)

	var n int
	var err error
	queued := runInBackground(func() { n, err = sink.Write([]byte("QUEUEDMSG\n")) })
	waitUntil(t, "the queued warning to wait behind the frozen writer", func() bool { return queuedWarnings(sink) == 1 })
	sink.Close()
	awaitBackground(t, queued, "the queued write did not return once Close dropped its entry")
	if !errors.Is(err, errWarningAbandoned) || n != 0 {
		t.Fatalf("the queued warning dropped at Close returned (%d, %v), want (0, errWarningAbandoned)", n, err)
	}
	awaitBackground(t, frozen, "the frozen write did not return")
	writer.open()
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(writer.out.String(), "QUEUEDMSG") {
		t.Fatalf("a warning reported as abandoned reached the output: %q", writer.out.String())
	}
}

func TestQueuedWarningUnconfirmedAtBudgetReturnsLateWithoutCount(t *testing.T) {
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	frozen := runInBackground(func() {
		if _, err := sink.Write([]byte("frozen\n")); !errors.Is(err, errWarningLate) {
			t.Errorf("write returned %v, want errWarningLate", err)
		}
	})
	awaitGatedWrite(t, writer)

	n, err := sink.Write([]byte("QUEUEDMSG\n"))
	if !errors.Is(err, errWarningLate) || n != 0 {
		t.Fatalf("a queued warning unconfirmed at the budget returned (%d, %v), want (0, errWarningLate)", n, err)
	}
	if got := droppedWarnings(sink); got != 0 {
		t.Fatalf("dropped = %d for an unconfirmed warning, want 0", got)
	}
	awaitBackground(t, frozen, "the frozen write did not return")
	writer.open()
}
