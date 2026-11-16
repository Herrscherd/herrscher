package host

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

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

func TestInFlightWriteFailingAfterCloseIsNotAnnouncedTwice(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newFirstWriteRefused()
	t.Cleanup(writer.unblock)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("INFLIGHT\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitBackground(t, writer.entered, "the warning was never handed to the writer")
	sink.Close()
	if err := sink.announce("HOLD"); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("the announce behind the in-flight write returned %v, want errWarningAbandoned", err)
	}
	writer.unblock()
	h := testHub(&hub{teardownGrace: 10 * time.Second})
	window, cancel := h.newGrace()
	defer cancel()
	if !sink.flush(window) {
		t.Fatal("the sink did not settle the in-flight write")
	}
	sink.announceAfterFinal()
	lines := notConfirmedLines(logs.String())
	if len(lines) != 2 || logField(lines[0], "lost") != "0" || logField(lines[0], "unconfirmed") != "1" || logField(lines[1], "lost") != "1" || logField(lines[1], "unconfirmed") != "0" {
		t.Fatalf("Close logged %q, want the bilan (lost=0 unconfirmed=1) then the abandoned announce (lost=1 unconfirmed=0), and nothing once the in-flight write failed: the failure after the bilan is counted, not announced again", lines)
	}
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

func TestSummaryFailingAfterCloseIsNotAnnouncedTwice(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newFirstWriteRefused()
	t.Cleanup(writer.unblock)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("oversized write returned %v, want errWarningAbandoned", err)
	}
	sink.Close()
	writer.unblock()
	waitUntil(t, "the summary refused after Close to settle", func() bool { return unconfirmedCount(sink) == 0 })
	lines := notConfirmedLines(logs.String())
	if strings.Count(logs.String(), "level=WARN") != 1 || len(lines) != 1 || logField(lines[0], "lost") != "1" || logField(lines[0], "unconfirmed") != "1" {
		t.Fatalf("Close logged %q, want exactly one line with lost=1 unconfirmed=1: the bilan announces the lost warning while its summary is in flight, and the refused summary is not announced again", lines)
	}
}

func TestRefusedSummaryBeforeCloseIsNotLoggedUntilClose(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := &summaryRefusingWriter{}
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("oversized write returned %v, want errWarningAbandoned", err)
	}
	if _, err := sink.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if logs.String() != "" {
		t.Fatalf("a summary refused before Close was logged %q, want nothing: the refusal is not final until Close", logs.String())
	}
	sink.Close()
	if want := englishLostLine(1) + "\n"; !strings.HasSuffix(writer.out.String(), want) {
		t.Fatalf("Close did not write the lost summary: output %q, want it to end with %q", writer.out.String(), want)
	}
	if logs.String() != "" {
		t.Fatalf("Close logged %q although its summary was written, want nothing", logs.String())
	}
}

func TestSummaryRefusedAtCloseIsAnnouncedOnceByTheBilan(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := &summaryRefusingWriter{always: true}
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("oversized write returned %v, want errWarningAbandoned", err)
	}
	sink.Close()
	lines := notConfirmedLines(logs.String())
	if strings.Count(logs.String(), "level=WARN") != 1 || len(lines) != 1 || logField(lines[0], "lost") != "1" || logField(lines[0], "unconfirmed") != "0" {
		t.Fatalf("Close logged %q, want exactly one line with lost=1 unconfirmed=0: the summary was definitively refused at Close, so the bilan is the one announcement", lines)
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

func TestWarningAfterCloseIsAnnouncedOnlyWithItsNewLoss(t *testing.T) {
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
	line := lastUnconfirmedLine(logs.String())
	if logField(line, "lost") != "1" || logField(line, "unconfirmed") != "0" {
		t.Fatalf("the lost overrun after Close logged %q, want lost=1 and unconfirmed=0: the bilan already announced the frozen write as unconfirmed", line)
	}
	if !strings.HasSuffix(line, `msg="warnings were not all confirmed when the sink closed" lost=1 unconfirmed=0`) {
		t.Fatalf("the lost overrun after Close logged %q, want the exact summary line with lost=1 unconfirmed=0", line)
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
