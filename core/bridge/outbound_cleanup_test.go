package bridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/outbox"
)

type panickingBackend struct {
	produce func(dir string) []contracts.OutboundFile
}

func (b panickingBackend) Respond(_ context.Context, p contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	b.produce(p.Outbox)
	panic("backend crashed mid-turn")
}

func (b panickingBackend) Close() error { return nil }

func (b panickingBackend) TakeFiles() []contracts.OutboundFile { return nil }

func TestTurnOutboxRemovedWhenBackendPanics(t *testing.T) {
	root := t.TempDir()
	backend := panickingBackend{produce: func(dir string) []contracts.OutboundFile {
		return []contracts.OutboundFile{outboundPNG(t, dir, "a.png", 8)}
	}}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("backend did not panic")
			}
		}()
		runOutboundTurn(t, backend, root)
	}()

	entries, err := os.ReadDir(filepath.Join(root, "chan"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("turn outboxes left after a panic: %v", entries)
	}
}

func TestEmitFilesAfterTurnEndDropsTheTurnDir(t *testing.T) {
	root := t.TempDir()
	rb := &recordingBackend{}
	turn := beginTurnOutbox(producingBackend{rb}, outboundSettings{root: root, session: "chan", maxBytes: outboundTestMaxBytes})
	dir := turn.dir
	rb.files = []contracts.OutboundFile{outboundPNG(t, dir, "a.png", 8)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sink := &recordSink{}

	turn.emitFiles(ctx, sink)

	if len(sink.events) != 0 {
		t.Fatalf("emitted %+v after the turn ended, want no files frame", sink.events)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("turn directory %q survived a cancelled turn: %v", dir, err)
	}
}

func TestBeginTurnOutboxPurgesOnlyItsOwnSession(t *testing.T) {
	root := t.TempDir()
	past := time.Now().Add(-outbox.MaxAge - time.Hour)
	stale := filepath.Join(root, "chan", "turn-stale")
	liveElsewhere := filepath.Join(root, "other", "turn-live")
	for _, dir := range []string{stale, liveElsewhere} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, past, past); err != nil {
			t.Fatal(err)
		}
	}

	turn := beginTurnOutbox(producingBackend{&recordingBackend{}}, outboundSettings{root: root, session: "chan", maxBytes: outboundTestMaxBytes})

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale turn directory %q of the session survived the start of a turn: %v", stale, err)
	}
	if _, err := os.Stat(liveElsewhere); err != nil {
		t.Fatalf("turn of another session was purged while it may still run: %v", err)
	}
	if _, err := os.Stat(turn.dir); err != nil {
		t.Fatalf("turn directory of the new turn missing: %v", err)
	}
}
