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
