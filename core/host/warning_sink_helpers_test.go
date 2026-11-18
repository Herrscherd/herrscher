package host

import (
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func englishLostLine(messages *hostMessages) func(lost int) string {
	return func(lost int) string {
		return messages.text(i18n.English, "warn.warnings_dropped", map[string]any{countParam: lost})
	}
}

func englishStillRunningLine(messages *hostMessages) string {
	return messages.text(i18n.English, "warn.write_still_running", nil)
}

func testHub(h *hub) *hub {
	if h.sessions == nil {
		h.sessions = newSessionTable(h.messages)
	}
	h.bindSessionState()
	out := h.warnings
	if out == nil {
		out = io.Discard
	}
	h.sink = newWarningSink(h.messages, out, englishLostLine(h.messages), slog.Default())
	h.warnings = h.sink
	return h
}

func droppedWarnings(s *warningSink) int {
	return ask(s, func() int { return s.dropped })
}

func queuedWarnings(s *warningSink) int {
	return ask(s, func() int { return len(s.queue) })
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

type summaryRefusingWriter struct {
	out     lockedBuffer
	refused bool
	always  bool
}

func (w *summaryRefusingWriter) Write(p []byte) (int, error) {
	if (w.always || !w.refused) && strings.Contains(string(p), "lost") {
		w.refused = true
		return 0, errors.New("the summary was refused")
	}
	return w.out.Write(p)
}

func unconfirmedLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "not all confirmed") {
			return line
		}
	}
	return ""
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
	return ask(s, func() int {
		if s.inflight != nil {
			return 1
		}
		return 0
	})
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

func expiresAfter(d time.Duration) <-chan struct{} {
	expired := make(chan struct{})
	time.AfterFunc(d, func() { close(expired) })
	return expired
}

func awaitBackground(t *testing.T, done <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal(failure)
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

func isClosing(s *warningSink) bool {
	return ask(s, func() bool { return s.closed })
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

func warnLinesOutsideBilan(messages *hostMessages, text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "level=WARN") && !strings.Contains(line, "not all confirmed") && !strings.Contains(line, englishStillRunningLine(messages)) {
			lines = append(lines, line)
		}
	}
	return lines
}

type hookedLog struct {
	buf  lockedBuffer
	hook func(line string)
}

func (h *hookedLog) Write(p []byte) (int, error) {
	if h.hook != nil {
		h.hook(string(p))
	}
	return h.buf.Write(p)
}

func (h *hookedLog) String() string {
	return h.buf.String()
}

func captureDefaultLog(t *testing.T, hook ...func(line string)) *hookedLog {
	t.Helper()
	logs := &hookedLog{}
	if len(hook) > 0 {
		logs.hook = hook[0]
	}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return logs
}

func logField(line, key string) string {
	for _, field := range strings.Fields(line) {
		if value, found := strings.CutPrefix(field, key+"="); found {
			return value
		}
	}
	return ""
}
