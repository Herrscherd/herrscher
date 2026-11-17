//go:build !windows

package host

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

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

func startBlockedTranscriptWrite(t *testing.T, h *hub, name string) <-chan struct{} {
	t.Helper()
	sink := h.liveSink(t.Context(), state.Session{Name: name, ChannelID: "c1"})
	written := make(chan struct{})
	go func() {
		defer close(written)
		sink.Transcript(state.TranscriptEntry{Role: "user", Text: "dernière écriture"})
	}()
	time.Sleep(100 * time.Millisecond)
	return written
}

func TestStopWaitsForAPersistentWriteInFlight(t *testing.T) {
	name := "persist-" + newTurnID()
	h := newInflightHub(t, name, time.Minute, io.Discard)
	path := makeBlockingTranscript(t, h, name)
	written := startBlockedTranscriptWrite(t, h, name)
	releaseTimer := time.AfterFunc(time.Second, func() { releaseBlockedTranscript(path) })
	t.Cleanup(func() { releaseTimer.Stop() })

	h.stopLive()

	select {
	case <-written:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("stopLive returned while the transcript write was still in flight: the last entry is lost")
	}
}
