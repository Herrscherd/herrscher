package host

import (
	"errors"
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
			writer := newGatedWarnings("blocked")
			sink := newWarningSink(writer, func(lost int) string {
				return newHostMessages().text(c.lang, "warn.warnings_dropped", map[string]any{countParam: lost})
			})
			if _, err := sink.Write([]byte("blocked\n")); err != nil {
				t.Fatal(err)
			}
			awaitGatedWrite(t, writer)
			for i := range warningQueueLimit {
				if _, err := sink.Write([]byte("item " + strconv.Itoa(i) + "\n")); err != nil {
					t.Fatal(err)
				}
			}
			const lost = 10
			for range lost {
				if _, err := sink.Write([]byte("overflow\n")); err != nil {
					t.Fatal(err)
				}
			}
			if got := sink.droppedCount(); got != lost {
				t.Fatalf("dropped %d warnings behind a full queue, want %d", got, lost)
			}

			writer.open()
			h := &hub{teardownGrace: 10 * time.Second}
			window, cancel := h.newGrace()
			defer cancel()
			if err := sink.Close(window); err != nil {
				t.Fatalf("Close after the writer unblocked: %v", err)
			}
			want := newHostMessages().text(c.lang, "warn.warnings_dropped", map[string]any{countParam: lost}) + "\n"
			if !strings.HasPrefix(writer.out.String(), "blocked\n"+want) {
				t.Fatalf("the summary of the %d lost warnings does not follow the write that unblocked:\n%s", lost, writer.out.String())
			}
			if strings.Contains(writer.out.String(), "overflow") {
				t.Fatal("a dropped warning reached the output")
			}
		})
	}
}

func TestWarningSinkRefusesWritesOnceClosed(t *testing.T) {
	var out lockedBuffer
	sink := newWarningSink(&out, englishLostLine)
	if _, err := sink.Write([]byte("before\n")); err != nil {
		t.Fatal(err)
	}
	h := &hub{teardownGrace: time.Second}
	window, cancel := h.newGrace()
	defer cancel()
	if err := sink.Close(window); err != nil {
		t.Fatalf("Close with an idle writer: %v", err)
	}
	if _, err := sink.Write([]byte("after\n")); !errors.Is(err, errWarningSinkClosed) {
		t.Fatalf("a write after Close returned %v, want errWarningSinkClosed", err)
	}
	if got := out.String(); got != "before\n" {
		t.Fatalf("output after Close = %q, want only the write before it", got)
	}
}

func TestWarningSinkCloseIsBoundedByAStalledWriter(t *testing.T) {
	writer := newGatedWarnings("stuck")
	t.Cleanup(writer.open)
	sink := newWarningSink(writer, englishLostLine)
	if _, err := sink.Write([]byte("stuck\n")); err != nil {
		t.Fatal(err)
	}
	h := &hub{teardownGrace: 5 * time.Second}
	window, cancel := h.newGrace()
	defer cancel()

	started := time.Now()
	if err := sink.Close(window); !errors.Is(err, errWarningsUndrained) {
		t.Fatalf("Close behind a stalled writer returned %v, want errWarningsUndrained", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Close held %v behind a stalled writer with a 5s grace, want under 1s", elapsed.Round(time.Millisecond))
	}
}
