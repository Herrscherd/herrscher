package tui

import (
	"context"
	"os"
	"testing"
)

func TestRunReturnsNilWhenItsContextIsCancelled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := reader.Close(); closeErr != nil {
			t.Log(closeErr)
		}
		if closeErr := writer.Close(); closeErr != nil {
			t.Log(closeErr)
		}
	})
	previous := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() { os.Stdin = previous })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, cancel, &fakeBackend{}); err != nil {
		t.Fatalf("Run with a cancelled context = %v, want nil: a quit that cancels the context is not a failure", err)
	}
}
