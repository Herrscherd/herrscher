package host

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
)

type streamedFailureError struct {
	text string
}

func (f streamedFailureError) Error() string { return "refused by the vendor" }

func (f streamedFailureError) PartialOutput() string { return f.text }

func TestThePartialOutputCrossesTheSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "p.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	failed := streamedFailureError{text: "partial answer"}
	serveCommandSocket(ctx, isolatedRuntimeDir(t), sock, dispatcherFunc(func(context.Context, []string) (string, error) {
		return "", failed
	}), testLanguage(), io.Discard)

	_, handled, err := dispatchLiveCommand(ctx, isolatedRuntimeDir(t), sock, []string{"session", "seed", "--name", "first"})
	partial, ok := errors.AsType[cli.PartialError](err)
	if !handled || !ok {
		t.Fatalf("socket returned handled=%v err=%v, want a partial failure", handled, err)
	}
	if got := partial.PartialOutput(); got != "partial answer" {
		t.Fatalf("PartialOutput() = %q, want the output produced before the failure", got)
	}
}
