package bridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
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
