package host

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
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
			if _, err := sink.Write([]byte("warning\n")); err != nil {
				t.Error(err)
			}
		}
	})
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("writes blocked behind a stuck writer")
	}

	if dropped := sink.droppedCount(); dropped < writes-warningQueueLimit-1 {
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
	if _, err := sink.Write([]byte("blocked\n")); err != nil {
		t.Fatal(err)
	}
	awaitGatedWrite(t, writer)
	fillQueueBehindBlockedWrite(t, sink)
	const lost = 10
	writeOverflow(t, sink, lost)
	if got := sink.droppedCount(); got != lost {
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
		if _, err := sink.Write([]byte("item " + strconv.Itoa(i) + "\n")); err != nil {
			t.Fatal(err)
		}
	}
}

func writeOverflow(t *testing.T, sink *warningSink, count int) {
	t.Helper()
	for range count {
		if _, err := sink.Write([]byte("overflow\n")); err != nil {
			t.Fatal(err)
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
	if _, err := sink.Write([]byte("stuck\n")); err != nil {
		t.Fatal(err)
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
	if _, err := sink.Write(oversized); err != nil {
		t.Fatal(err)
	}
	if got := sink.droppedCount(); got != 1 {
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
	if _, err := sink.Write([]byte("blocked\n")); err != nil {
		t.Fatal(err)
	}
	awaitGatedWrite(t, writer)
	entry := bytes.Repeat([]byte("y"), warningEntryBytes)
	for range warningQueueBytes / warningEntryBytes {
		if _, err := sink.Write(entry); err != nil {
			t.Fatal(err)
		}
	}
	if got := sink.droppedCount(); got != 0 {
		t.Fatalf("dropped %d entries within the byte budget, want 0", got)
	}
	if _, err := sink.Write(entry); err != nil {
		t.Fatal(err)
	}
	if got := sink.droppedCount(); got != 1 {
		t.Fatalf("dropped %d entries past the byte budget, want 1", got)
	}
}

func TestLateWritesBehindAFrozenWriterAreLostWithoutAGoroutine(t *testing.T) {
	writer := newGatedWarnings("stuck")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("stuck\n")); err != nil {
		t.Fatal(err)
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
	if dropped := sink.droppedCount(); dropped != 20 {
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
		if _, err := h.sink.Write([]byte("queued\n")); err != nil {
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
	before := sink.droppedCount()
	if err := sink.announce("overrun"); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("an overrun behind a frozen writer returned %v, want errWarningAbandoned", err)
	}
	if got := sink.droppedCount(); got != before+1 {
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
	if _, err := sink.Write([]byte("frozen\n")); err != nil {
		t.Fatal(err)
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
	if got := sink.droppedCount(); got != 2 {
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
	if dropped := sink.droppedCount(); dropped != 0 {
		t.Fatalf("an unconfirmed write was counted as lost: dropped %d, want 0", dropped)
	}
}

func TestCloseReportsTheFinalCountWhenItsSummaryCannotBeWritten(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("frozen\n")); err != nil {
		t.Fatal(err)
	}
	awaitGatedWrite(t, writer)
	if err := sink.announce("HOLD"); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("the announce behind the frozen writer returned %v, want errWarningAbandoned", err)
	}
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); err != nil {
		t.Fatal(err)
	}
	sink.Close()
	line := unconfirmedLine(logs.String())
	if !strings.Contains(line, "lost=2") || !strings.Contains(line, "unconfirmed=1") {
		t.Fatalf("Close logged %q, want lost=2 and unconfirmed=1: the announce and the oversized warning are lost, the frozen write is unconfirmed", line)
	}
}

func TestFailedSummaryStaysUnreportedUntilItIsWritten(t *testing.T) {
	writer := &summaryRefusingWriter{}
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); err != nil {
		t.Fatal(err)
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
	if _, err := sink.Write([]byte("frozen\n")); err != nil {
		t.Fatal(err)
	}
	awaitGatedWrite(t, writer)
	before := sink.droppedCount()
	if err := sink.announce("OVERRUN"); !errors.Is(err, errWarningAbandoned) && !errors.Is(err, errWarningLate) {
		t.Fatalf("the overrun behind a frozen drain returned %v", err)
	}
	sink.Close()
	lost := sink.droppedCount() - before
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
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Write([]byte("A\n")); err != nil {
		t.Fatal(err)
	}
	awaitGatedWrite(t, writer)
	sink.Close()
	line := unconfirmedLine(logs.String())
	if !strings.Contains(line, "lost=1") || unconfirmedAtClose(t, logs.String()) < 1 {
		t.Fatalf("Close logged %q, want lost=1 and unconfirmed>=1 while the summary is in flight", line)
	}
}

func TestCloseNamesAnUnconfirmedWriteEvenWhenNothingWasLost(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("frozen\n")); err != nil {
		t.Fatal(err)
	}
	awaitGatedWrite(t, writer)
	sink.Close()
	line := unconfirmedLine(logs.String())
	if !strings.Contains(line, "lost=0") || !strings.Contains(line, "unconfirmed=1") {
		t.Fatalf("Close logged %q, want lost=0 and unconfirmed=1: the frozen write is in flight and nothing was dropped", line)
	}
}

func TestQueuedBehindAFrozenWriterIsLostAtCloseAndNeverWritten(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("frozen\n")); err != nil {
		t.Fatal(err)
	}
	awaitGatedWrite(t, writer)
	if _, err := sink.Write([]byte("QUEUEDMSG\n")); err != nil {
		t.Fatal(err)
	}
	sink.Close()
	if got := sink.droppedCount(); got != 1 {
		t.Fatalf("dropped %d warnings at Close, want 1: the queued warning was never handed to the writer", got)
	}
	line := unconfirmedLine(logs.String())
	if !strings.Contains(line, "lost=1") || !strings.Contains(line, "unconfirmed=1") {
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
