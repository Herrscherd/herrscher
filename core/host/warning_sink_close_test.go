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
		t.Fatalf("Close logged %q, want lost=1 and unconfirmed>=1: the summary in flight already names the oversized warning, only the queued A is left for the bilan", line)
	}
}

func TestSummaryAcceptedAfterCloseDoesNotCountLossesTwice(t *testing.T) {
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
	writer.open()
	waitUntil(t, "the summary to be accepted after the bilan", func() bool {
		return ask(sink, func() bool { return sink.inflight == nil })
	})
	if got := strings.Count(writer.out.String(), englishLostLine(1)+"\n"); got != 1 {
		t.Fatalf("the summary reached the output %d times, want once: %q", got, writer.out.String())
	}
	awaitLogIdle(t, &sink.log)
	lines := notConfirmedLines(logs.String())
	if len(lines) != 1 || logField(lines[0], "lost") != "1" {
		t.Fatalf("the bilan logged %q, want only the queued A (lost=1): the oversized warning is already named by the summary that Close let through", lines)
	}
	if _, err := sink.Write(bytes.Repeat([]byte("y"), warningEntryBytes+1)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("write returned %v, want errWarningAbandoned", err)
	}
	lines = notConfirmedLines(logs.String())
	if len(lines) != 2 || logField(lines[1], "lost") != "1" || logField(lines[1], "unconfirmed") != "0" {
		t.Fatalf("logged %q, want the bilan then lost=1 unconfirmed=0 for the new loss alone", lines)
	}
}

func TestCloseWaitsForAWriteThatFinishesWithinTheBudget(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	_ = sink.Enqueue([]byte("frozen\n"))
	awaitGatedWrite(t, writer)
	go func() {
		time.Sleep(10 * time.Millisecond)
		writer.open()
	}()
	sink.Close()
	if lines := notConfirmedLines(logs.String()); len(lines) != 0 {
		t.Fatalf("Close logged %q, want nothing: the write finished inside the budget, so Close must wait for it", lines)
	}
	if !strings.Contains(writer.out.String(), "frozen") {
		t.Fatalf("the queued warning was not written, output %q", writer.out.String())
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

func TestInFlightWriteFailingAfterCloseIsAnnouncedAsADelta(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newFirstWriteRefused()
	t.Cleanup(writer.unblock)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("INFLIGHT\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitBackground(t, writer.entered, "the warning was never handed to the writer")
	time.AfterFunc(warningWriteBudget+warningWriteBudget/2, writer.unblock)
	sink.Close()
	lines := notConfirmedLines(logs.String())
	if len(lines) != 2 || logField(lines[0], "lost") != "0" || logField(lines[0], "unconfirmed") != "1" || logField(lines[1], "lost") != "1" || logField(lines[1], "unconfirmed") != "0" {
		t.Fatalf("Close logged %q, want the bilan (lost=0 unconfirmed=1) then one delta (lost=1 unconfirmed=0) once the in-flight write failed: the failure after the bilan is a new loss, announced once", lines)
	}
}

func TestRefusedWriteAfterTheBilanIsLoggedBeforeCloseReturns(t *testing.T) {
	writer := newFirstWriteRefused()
	t.Cleanup(writer.unblock)
	var sink *warningSink
	refused := make(chan error, 1)
	logs := captureDefaultLog(t, func(line string) {
		if strings.Contains(line, "not all confirmed") && logField(line, "unconfirmed") == "1" {
			go func() {
				_, err := sink.Write([]byte("AFTER\n"))
				refused <- err
				writer.unblock()
			}()
		}
	})
	sink = newWarningSink(writer, englishLostLine)
	_ = sink.Enqueue([]byte("INFLIGHT\n"))
	awaitBackground(t, writer.entered, "the warning was never handed to the writer")
	sink.Close()
	var afterErr error
	select {
	case afterErr = <-refused:
	case <-expiresAfter(5 * time.Second):
		t.Fatal("Close returned before the write admitted after the bilan was refused")
	}
	if !errors.Is(afterErr, errWarningAbandoned) {
		t.Fatalf("a write admitted after the bilan returned %v, want errWarningAbandoned", afterErr)
	}
	lines := notConfirmedLines(logs.String())
	if len(lines) < 2 || logField(lines[0], "unconfirmed") != "1" || logField(lines[1], "lost") != "1" || logField(lines[1], "unconfirmed") != "0" {
		t.Fatalf("Close logged %q, want the bilan (unconfirmed=1) then the refused write (lost=1 unconfirmed=0) before it returned", lines)
	}
	waitUntil(t, "the delta of the older write once its writer is released", func() bool {
		return len(notConfirmedLines(logs.String())) == 3
	})
	lines = notConfirmedLines(logs.String())
	if logField(lines[2], "lost") != "1" || logField(lines[2], "unconfirmed") != "0" {
		t.Fatalf("the delta of the older write is %q, want lost=1 unconfirmed=0 once the writer returned", lines[2])
	}
	if strings.Contains(writer.out.String(), "AFTER") {
		t.Fatalf("a write refused after the bilan reached the output: %q", writer.out.String())
	}
}

func TestEntryAdmittedWhileCloseWaitsIsCountedOnceByTheBilan(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("frozen")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine, withIdleWait(2*time.Second))
	_ = sink.Enqueue([]byte("frozen\n"))
	awaitGatedWrite(t, writer)

	closed := runInBackground(sink.Close)
	waitUntil(t, "Close to begin while the frozen write is in flight", func() bool { return isClosing(sink) })
	queued := sink.Enqueue([]byte("QUEUEDMSG\n"))
	if err := queued(expiresAfter(10 * time.Second)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("an entry admitted while Close waited returned %v, want errWarningAbandoned once the bilan sealed", err)
	}
	awaitBackground(t, closed, "Close did not return")

	lines := notConfirmedLines(logs.String())
	if len(lines) != 1 || logField(lines[0], "lost") != "1" || logField(lines[0], "unconfirmed") != "1" {
		t.Fatalf("Close logged %q, want exactly one bilan with lost=1 unconfirmed=1: the entry is counted once, by the bilan, and the frozen write is unconfirmed", lines)
	}
	if got := droppedWarnings(sink); got != 1 {
		t.Fatalf("dropped = %d, want 1: the entry admitted between closed and sealed is lost once", got)
	}
	writer.open()
	awaitWarning(t, &writer.out, "frozen\n")
	waitUntil(t, "the frozen write to settle", func() bool {
		return ask(sink, func() bool { return sink.inflight == nil })
	})
	if strings.Contains(writer.out.String(), "QUEUEDMSG") {
		t.Fatalf("an entry counted as lost by the bilan reached the output: %q", writer.out.String())
	}
	if lines := notConfirmedLines(logs.String()); len(lines) != 1 {
		t.Fatalf("a second line appeared after the writer settled: %q, want the bilan alone", lines)
	}
}

func TestCloseSaysWhenAWriteIsStillRunningAtReturn(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("GATED")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine, withIdleWait(time.Millisecond))
	_ = sink.Enqueue([]byte("GATED\n"))
	awaitGatedWrite(t, writer)
	sink.Close()
	if n := strings.Count(logs.String(), englishStillRunningLine()); n != 1 {
		t.Fatalf("Close returned with its last write still running and logged the note %d times, want once: %q", n, logs.String())
	}
}

func TestCloseSaysNothingWhenTheLastWriteIsJoined(t *testing.T) {
	logs := captureDefaultLog(t)
	var out lockedBuffer
	sink := newWarningSink(&out, englishLostLine)
	if _, err := sink.Write([]byte("done\n")); err != nil {
		t.Fatal(err)
	}
	sink.Close()
	if strings.Contains(logs.String(), englishStillRunningLine()) {
		t.Fatalf("Close logged that a write was still running although its last write was joined: %q", logs.String())
	}
}

func TestConcurrentSecondCloseReturnsOnlyAfterTheBilanIsLogged(t *testing.T) {
	logs := captureDefaultLog(t, func(line string) {
		if strings.Contains(line, "not all confirmed") {
			time.Sleep(warningWriteBudget / 2)
		}
	})
	writer := newFirstWriteRefused()
	t.Cleanup(writer.unblock)
	sink := newWarningSink(writer, englishLostLine)
	_ = sink.Enqueue([]byte("INFLIGHT\n"))
	awaitBackground(t, writer.entered, "the warning was never handed to the writer")
	first := make(chan struct{})
	go func() {
		sink.Close()
		close(first)
	}()
	waitUntil(t, "the first Close to begin", func() bool { return isClosing(sink) })
	sink.Close()
	if !strings.Contains(logs.String(), "not all confirmed") {
		t.Fatal("the second Close returned before the bilan was logged: it must wait for the first Close to finish")
	}
	awaitBackground(t, first, "the first Close did not return")
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
	waitUntil(t, "Close to begin while the in-flight write is still blocked", func() bool { return isClosing(sink) })
	writer.unblock()
	awaitBackground(t, closed, "the background Close did not return")
	if !strings.Contains(writer.out.String(), englishLostLine(2)+"\n") {
		t.Fatalf("the failed write during Close was not followed by the summary of 2 lost warnings: output %q", writer.out.String())
	}
	if lines := notConfirmedLines(logs.String()); len(lines) != 0 {
		t.Fatalf("closing logged %q, want no bilan: the summary was written before the sink sealed", lines)
	}
}

func TestSummaryFailingAfterCloseIsAnnouncedOnceAsADelta(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newFirstWriteRefused()
	t.Cleanup(writer.unblock)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("oversized write returned %v, want errWarningAbandoned", err)
	}
	sink.Close()
	writer.unblock()
	waitUntil(t, "the summary refused after Close to be announced as a delta", func() bool {
		return len(notConfirmedLines(logs.String())) == 2
	})
	lines := notConfirmedLines(logs.String())
	if len(lines) != 2 || logField(lines[0], "lost") != "0" || logField(lines[0], "unconfirmed") != "1" || logField(lines[1], "lost") != "1" || logField(lines[1], "unconfirmed") != "0" {
		t.Fatalf("Close logged %q, want the bilan (lost=0 unconfirmed=1) then one delta (lost=1 unconfirmed=0): the bilan names the refused summary as unconfirmed, and its loss is announced once when the summary fails", lines)
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
	if len(lines) != 1 || logField(lines[0], "lost") != "1" || logField(lines[0], "unconfirmed") != "0" {
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
	awaitLogIdle(t, &sink.log)
	before := strings.Count(logs.String(), "not all confirmed")

	if err := sink.announce("OVERRUN"); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("the overrun behind a frozen writer after Close returned %v, want errWarningAbandoned", err)
	}
	awaitLogIdle(t, &sink.log)
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

func TestQueuedBehindAStalledWriterIsLateAndLostAtClose(t *testing.T) {
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
	awaitBackground(t, queued, "the queued write did not return once its budget expired")
	if !errors.Is(err, errWarningLate) || n != 0 {
		t.Fatalf("the queued warning behind the frozen writer returned (%d, %v), want (0, errWarningLate)", n, err)
	}
	sink.Close()
	if got := droppedWarnings(sink); got != 1 {
		t.Fatalf("dropped = %d at Close, want 1: the queued warning was never handed to the writer", got)
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

func TestAdmissionAfterTheBilanStartsNoWriteAndCountsItsLoss(t *testing.T) {
	logs := captureDefaultLog(t)
	var out lockedBuffer
	sink := newWarningSink(&out, englishLostLine)
	if _, err := sink.Write([]byte("before\n")); err != nil {
		t.Fatal(err)
	}
	sink.Close()
	lastBefore := ask(sink, func() chan struct{} { return sink.lastLogged })

	if _, err := sink.Write([]byte("after\n")); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("a write admitted after the bilan returned %v, want errWarningAbandoned", err)
	}
	if busy := ask(sink, func() bool { return sink.inflight != nil }); busy {
		t.Fatal("a write admitted after the bilan started an I/O goroutine")
	}
	if lastAfter := ask(sink, func() chan struct{} { return sink.lastLogged }); lastAfter != lastBefore {
		t.Fatal("a write refused after the bilan replaced the last write that Close joins")
	}
	if out.String() != "before\n" {
		t.Fatalf("output = %q, want only the write before Close", out.String())
	}
	if n := strings.Count(logs.String(), "not all confirmed"); n != 1 {
		t.Fatalf("logged %d unconfirmed lines for the refused admission, want 1: %q", n, logs.String())
	}
	line := lastUnconfirmedLine(logs.String())
	if logField(line, "lost") != "1" || logField(line, "unconfirmed") != "0" {
		t.Fatalf("the refused admission logged %q, want lost=1 and unconfirmed=0", line)
	}
}

func TestCloseJoinsTheWriteLaunchedLast(t *testing.T) {
	warnings := newGatedWarnings("GATED")
	t.Cleanup(warnings.open)
	sink := newWarningSink(warnings, englishLostLine)
	if _, err := sink.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	firstLogged := ask(sink, func() chan struct{} { return sink.lastLogged })
	awaitBackground(t, firstLogged, "the first write never finished logging")

	sink.Enqueue([]byte("GATED\n"))
	awaitGatedWrite(t, warnings)
	gatedLogged := ask(sink, func() chan struct{} { return sink.lastLogged })
	if gatedLogged == firstLogged {
		t.Fatal("the write launched last is not the one Close joins: lastLogged still names the first write")
	}
	select {
	case <-gatedLogged:
		t.Fatal("the write launched last is already logged while its writer is still blocked")
	default:
	}
	warnings.open()
	awaitBackground(t, gatedLogged, "the gated write never finished logging")
}

type slowWarnings struct {
	out lockedBuffer
}

func (s *slowWarnings) Write(p []byte) (int, error) {
	time.Sleep(50 * time.Millisecond)
	return s.out.Write(p)
}

func TestCloseReturnsOnlyAfterTheLastLaunchedWriteIsOnOutput(t *testing.T) {
	warnings := &slowWarnings{}
	sink := newWarningSink(warnings, englishLostLine, withIdleWait(time.Nanosecond))
	_ = sink.Enqueue([]byte("LAST\n"))
	sink.Close()
	if !strings.Contains(warnings.out.String(), "LAST\n") {
		t.Fatalf("Close returned before the write launched last reached the output: %q", warnings.out.String())
	}
}
