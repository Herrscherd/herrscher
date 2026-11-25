//go:build !windows

package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/checkpoint"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func makeBlockingTranscript(t *testing.T, h *hub, name string) string {
	t.Helper()
	path := state.TranscriptPath(h.partDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func releaseBlockedTranscript(path string) {
	go func() {
		reader, err := os.Open(path)
		if err != nil {
			logCleanup(err)
			return
		}
		_, copyErr := io.Copy(io.Discard, reader)
		logCleanup(copyErr)
		logCleanup(reader.Close())
	}()
}

func unblockTranscript(path string) {
	writer, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		logCleanup(err)
		return
	}
	logCleanup(writer.Close())
}

func awaitPersistWriteInFlight(t *testing.T, h *hub) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for h.persists.group.pending() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the write never entered the persist group: its caller is not wired to the write tracker")
		}
		time.Sleep(time.Millisecond)
	}
}

func startBlockedTranscriptWrite(t *testing.T, h *hub, name string) <-chan struct{} {
	t.Helper()
	sink := h.liveSink(t.Context(), state.Session{Name: name, ChannelID: "c1"})
	written := make(chan struct{})
	go func() {
		defer close(written)
		logCleanup(sink.Transcript(state.TranscriptEntry{Role: "user", Text: "dernière écriture"}))
	}()
	awaitPersistWriteInFlight(t, h)
	return written
}

func persistOverrunText(h *hub) string {
	return newHostMessages().text(h.textLanguage()(), "warn.persist_overrun", map[string]any{graceParam: persistWriteGrace.String()})
}

func TestStopWaitsForAPersistentWriteInFlight(t *testing.T) {
	name := "persist-" + newTurnID()
	h := newInflightHub(t, name, time.Minute, io.Discard)
	path := makeBlockingTranscript(t, h, name)
	written := startBlockedTranscriptWrite(t, h, name)
	releaseTimer := time.AfterFunc(time.Second, func() { releaseBlockedTranscript(path) })
	t.Cleanup(func() {
		releaseTimer.Stop()
		unblockTranscript(path)
	})

	h.stopLive()

	select {
	case <-written:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("stopLive returned while the transcript write was still in flight: the last entry is lost")
	}
}

func TestPersistWriteInFlightIsAwaitedByEveryStopPath(t *testing.T) {
	stops := []struct {
		name string
		stop func(h *hub, name string)
	}{
		{name: "stopLive", stop: func(h *hub, _ string) { h.stopLive() }},
		{name: "closeAndRemoveSockets", stop: func(h *hub, _ string) { logCleanup(h.closeAndRemoveSockets()) }},
		{name: "goDead then stopLive", stop: func(h *hub, name string) {
			h.goDead(name)
			h.stopLive()
		}},
	}
	for _, c := range stops {
		t.Run(c.name, func(t *testing.T) {
			name := "persist-" + newTurnID()
			h := newInflightHub(t, name, time.Minute, io.Discard)
			path := makeBlockingTranscript(t, h, name)
			written := startBlockedTranscriptWrite(t, h, name)
			releaseTimer := time.AfterFunc(time.Second, func() { releaseBlockedTranscript(path) })
			t.Cleanup(func() {
				releaseTimer.Stop()
				unblockTranscript(path)
			})

			c.stop(h, name)

			select {
			case <-written:
			case <-time.After(300 * time.Millisecond):
				t.Fatal("the stop returned while the transcript write was still in flight: the last entry is lost")
			}
		})
	}
}

func TestStopStaysBoundedWhenAPersistentWriteNeverEnds(t *testing.T) {
	name := "persist-" + newTurnID()
	warnings := &lockedBuffer{}
	h := newInflightHub(t, name, time.Minute, warnings)
	path := makeBlockingTranscript(t, h, name)
	startBlockedTranscriptWrite(t, h, name)
	t.Cleanup(func() { unblockTranscript(path) })

	started := time.Now()
	returned := runInBackground(h.stopLive)
	select {
	case <-returned:
	case <-time.After(persistWriteGrace + 10*time.Second):
		t.Fatal("stopLive never returned while a persistent write never ended")
	}
	if elapsed := time.Since(started); elapsed > persistWriteGrace+5*time.Second {
		t.Fatalf("stopLive took %v with a stuck persistent write, want about %v", elapsed, persistWriteGrace)
	}
	if !strings.Contains(warnings.String(), persistOverrunText(h)) {
		t.Fatalf("stopLive went on without warning about the stuck write: %q", warnings.String())
	}
}

func TestStopAndSocketCloseShareOnePersistBudget(t *testing.T) {
	name := "persist-" + newTurnID()
	warnings := &lockedBuffer{}
	h := newInflightHub(t, name, time.Minute, warnings)
	path := makeBlockingTranscript(t, h, name)
	startBlockedTranscriptWrite(t, h, name)
	t.Cleanup(func() { unblockTranscript(path) })

	started := time.Now()
	h.stopLive()
	logCleanup(h.closeAndRemoveSockets())
	if elapsed := time.Since(started); elapsed >= persistWriteGrace+time.Second {
		t.Fatalf("stop then socket close took %v with a stuck write: the persist budget was spent twice, want about %v", elapsed, persistWriteGrace)
	}
	if got := strings.Count(warnings.String(), persistOverrunText(h)); got != 1 {
		t.Fatalf("the persist overrun was announced %d times, want exactly once: %q", got, warnings.String())
	}
}

type blockingCheckpoints struct {
	entered     chan struct{}
	release     chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

func newBlockingCheckpoints() *blockingCheckpoints {
	return &blockingCheckpoints{entered: make(chan struct{}), release: make(chan struct{})}
}

func (b *blockingCheckpoints) Take(context.Context, string, string, checkpoint.Meta) (checkpoint.Entry, error) {
	b.enterOnce.Do(func() { close(b.entered) })
	<-b.release
	return checkpoint.Entry{}, nil
}

func (b *blockingCheckpoints) unblock() {
	b.releaseOnce.Do(func() { close(b.release) })
}

func TestStopWaitsForACheckpointInFlight(t *testing.T) {
	name := "persist-" + newTurnID()
	h := newInflightHub(t, name, time.Minute, io.Discard)
	taking := newBlockingCheckpoints()
	h.checkpoints = taking
	t.Cleanup(taking.unblock)
	sess := state.Session{Name: name, ChannelID: "c1", Worktree: t.TempDir()}
	taken := runInBackground(func() { h.takeCheckpoint(t.Context(), sess, "turn-1", "texte") })
	select {
	case <-taking.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the checkpoint was never taken")
	}
	releaseTimer := time.AfterFunc(time.Second, taking.unblock)
	t.Cleanup(func() { releaseTimer.Stop() })

	h.stopLive()

	select {
	case <-taken:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("stopLive returned while the checkpoint was still being taken")
	}
}

func TestWriteStartedAfterThePersistDeadlineIsAnnounced(t *testing.T) {
	name := "persist-" + newTurnID()
	warnings := &lockedBuffer{}
	h := newInflightHub(t, name, time.Minute, warnings)
	h.awaitPersists()
	select {
	case <-h.persists.deadline():
	case <-time.After(5 * time.Second):
		t.Fatal("the persist deadline never expired")
	}
	if err := os.MkdirAll(filepath.Dir(state.TranscriptPath(h.partDir, name)), 0o700); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := h.appendTranscript(name, state.TranscriptEntry{Role: "user", Text: "tardive"}); err != nil {
			t.Fatal(err)
		}
	}

	if got := strings.Count(warnings.String(), persistOverrunText(h)); got != 1 {
		t.Fatalf("a write started after the deadline was announced %d times, want exactly once: %q", got, warnings.String())
	}
	landed := state.ReadTranscript(state.TranscriptPath(h.partDir, name), 10)
	if len(landed) != 2 {
		t.Fatalf("the writes started after the deadline left %d entries in the transcript, want 2: they are performed, not cut", len(landed))
	}
}
