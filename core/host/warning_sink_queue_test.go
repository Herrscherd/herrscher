package host

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestWarningSinkDoesNotBlockWritersAndCountsDrops(t *testing.T) {
	writer := newGatedWarnings("blocked")
	t.Cleanup(writer.open)
	messages := newHostMessages()
	sink := newTestWarningSink(t, messages, writer, englishLostLine(messages), discardLogger())
	if _, err := sink.Write([]byte("blocked\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, writer)

	const writes = 1000
	returned := runInBackground(func() {
		for range writes {
			if _, err := sink.Write([]byte("warning\n")); !isWarningOutcome(err) {
				t.Error(err)
			}
		}
	})
	awaitBackground(t, returned, "writes blocked behind a stuck writer")

	if dropped, want := droppedWarnings(sink), writes-warningQueueLimit; dropped != want {
		t.Fatalf("dropped %d of %d writes behind a stuck writer, want exactly %d: the queue holds %d and the blocked write is in flight", dropped, writes, want, warningQueueLimit)
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
	messages := newHostMessages()
	sink := newTestWarningSink(t, messages, writer, func(lost int) string {
		return newHostMessages().text(lang, "warn.warnings_dropped", map[string]any{countParam: lost})
	}, discardLogger())
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
	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), teardownGrace: 10 * time.Second})
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

func TestWarningSinkRefusesWritesAfterCloseAndCountsThem(t *testing.T) {
	var out lockedBuffer
	messages := newHostMessages()
	sink := newTestWarningSink(t, messages, &out, englishLostLine(messages), discardLogger())
	if _, err := sink.Write([]byte("before\n")); err != nil {
		t.Fatal(err)
	}
	sink.Close()
	if _, err := sink.Write([]byte("after\n")); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("a write after Close returned %v, want errWarningAbandoned: the bilan is sealed", err)
	}
	if got := out.String(); got != "before\n" {
		t.Fatalf("output after Close = %q, want only the write before it", got)
	}
	if got := droppedWarnings(sink); got != 1 {
		t.Fatalf("dropped %d warnings after Close, want 1", got)
	}
}

func TestWarningSinkAfterCloseIsBoundedByAStalledWriter(t *testing.T) {
	writer := newGatedWarnings("stuck")
	t.Cleanup(writer.open)
	messages := newHostMessages()
	sink := newTestWarningSink(t, messages, writer, englishLostLine(messages), discardLogger())
	sink.Close()

	started := time.Now()
	if _, err := sink.Write([]byte("stuck\n")); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("a write behind a stalled writer after Close returned %v, want errWarningAbandoned", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("a write after Close held %v behind a stalled writer, want under 1s", elapsed.Round(time.Millisecond))
	}
}

func TestWarningSinkFlushIsBoundedByAStalledWriter(t *testing.T) {
	writer := newGatedWarnings("stuck")
	t.Cleanup(writer.open)
	messages := newHostMessages()
	sink := newTestWarningSink(t, messages, writer, englishLostLine(messages), discardLogger())
	if _, err := sink.Write([]byte("stuck\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), teardownGrace: 5 * time.Second})
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
	messages := newHostMessages()
	sink := newTestWarningSink(t, messages, &out, englishLostLine(messages), discardLogger())
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
	if want := englishLostLine(messages)(1) + "\nafter\n"; out.String() != want {
		t.Fatalf("output = %q, want the summary of the lost warning then the next one %q", out.String(), want)
	}
}

func TestWarningSinkCountsAnEntryOverTheByteBudgetAsLost(t *testing.T) {
	writer := newGatedWarnings("blocked")
	t.Cleanup(writer.open)
	messages := newHostMessages()
	sink := newTestWarningSink(t, messages, writer, englishLostLine(messages), discardLogger())
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
	messages := newHostMessages()
	sink := newTestWarningSink(t, messages, writer, englishLostLine(messages), discardLogger())
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
	if grown := runtime.NumGoroutine() - before; grown > 1 {
		t.Fatalf("20 late writes that never got the writer added %d goroutines, want at most the one bounded log writer", grown)
	}
	if dropped := droppedWarnings(sink); dropped != 20 {
		t.Fatalf("%d late writes were abandoned without being counted as lost, want 20", dropped)
	}
	writer.open()
	awaitSinkLog(t, sink)
	waitUntil(t, "the goroutines of the late writes to end once the writer is released", func() bool {
		return runtime.NumGoroutine() <= before
	})
}

func TestLateWriteKeepsTheBytesItWasGiven(t *testing.T) {
	writer := newGatedWarnings("LATEMSG")
	t.Cleanup(writer.open)
	messages := newHostMessages()
	sink := newTestWarningSink(t, messages, writer, englishLostLine(messages), discardLogger())
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

func TestFailedSummaryStaysUnreportedUntilItIsWritten(t *testing.T) {
	writer := &summaryRefusingWriter{}
	messages := newHostMessages()
	sink := newTestWarningSink(t, messages, writer, englishLostLine(messages), discardLogger())
	if _, err := sink.Write(bytes.Repeat([]byte("x"), warningEntryBytes+1)); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("write returned %v, want errWarningAbandoned", err)
	}
	if _, err := sink.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Write([]byte("second\n")); err != nil {
		t.Fatal(err)
	}
	want := "first\n" + englishLostLine(messages)(1) + "\nsecond\n"
	if got := writer.out.String(); got != want {
		t.Fatalf("output = %q, want %q: a refused summary is written again before the next warning", got, want)
	}
}

func TestQueuedWriterErrorIsCountedLostBeforeTheNextWrite(t *testing.T) {
	writer := &refusingEntryWriter{marker: "REFUSED"}
	messages := newHostMessages()
	sink := newTestWarningSink(t, messages, writer, englishLostLine(messages), discardLogger())
	if _, err := sink.Write([]byte("REFUSED\n")); !errors.Is(err, errEntryRefused) {
		t.Fatalf("the refused queued warning returned %v, want the writer's error", err)
	}
	if _, err := sink.Write([]byte("after\n")); err != nil {
		t.Fatalf("the write after the refused one returned %v, want nil", err)
	}
	sink.Close()
	if want := englishLostLine(messages)(1) + "\nafter\n"; writer.out.String() != want {
		t.Fatalf("output = %q, want %q: the refused queued warning must be counted lost and announced before the next write", writer.out.String(), want)
	}
}
