package host

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

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
	_ = sink.Enqueue([]byte("late\n"))
	awaitGatedWrite(t, writer)

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
	awaitSinkQuiet(t, sink)
	if strings.Contains(writer.out.String(), "overrun") {
		t.Fatalf("an overrun counted as lost reached the output after the writer unblocked: %q", writer.out.String())
	}
}

func TestAbandonedOverrunIsSilentUntilTheBilan(t *testing.T) {
	logs := captureDefaultLog(t)
	writer := newGatedWarnings("slow")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	_ = sink.Enqueue([]byte("slow\n"))
	awaitGatedWrite(t, writer)
	if err := sink.announce("overrun"); !errors.Is(err, errWarningAbandoned) {
		t.Fatalf("an overrun behind a slow write returned %v, want errWarningAbandoned", err)
	}
	awaitSinkLog(t, sink)
	if logs.String() != "" {
		t.Fatalf("an abandoned overrun logged %q before Close: only the bilan announces it", logs.String())
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
	awaitSinkQuiet(t, sink)
	if out := writer.out.String(); strings.Contains(out, "FIRST") || strings.Contains(out, "SECOND") {
		t.Fatalf("an announce counted as lost reached the output: %q", out)
	}
}
