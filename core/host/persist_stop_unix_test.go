//go:build !windows

package host

import (
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestStopStaysBoundedWhenAPersistentWriteNeverEnds(t *testing.T) {
	name := "persist-" + newTurnID()
	warnings := &lockedBuffer{}
	h := newInflightHub(t, name, time.Minute, warnings)
	path := makeBlockingTranscript(t, h, name)
	startBlockedTranscriptWrite(t, h, name)
	t.Cleanup(func() { releaseBlockedTranscript(path) })

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
	want := newHostMessages().text(h.textLanguage()(), "warn.persist_overrun", map[string]any{graceParam: persistWriteGrace.String()})
	if !strings.Contains(warnings.String(), want) {
		t.Fatalf("stopLive went on without warning about the stuck write: %q", warnings.String())
	}
}
