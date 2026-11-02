package bridge

import (
	"io"
	"log/slog"
	"os"
	"testing"
)

func writeOrLog(w io.Writer, b []byte) {
	if _, err := w.Write(b); err != nil {
		slog.Debug("write", "err", err)
	}
}

func removeAllOrFail(tb testing.TB, path string) {
	tb.Helper()
	if err := os.RemoveAll(path); err != nil {
		tb.Errorf("remove %s: %v", path, err)
	}
}

func mustReadFile(tb testing.TB, name string) []byte {
	tb.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func mustReadDir(tb testing.TB, name string) []os.DirEntry {
	tb.Helper()
	entries, err := os.ReadDir(name)
	if err != nil {
		tb.Fatal(err)
	}
	return entries
}
