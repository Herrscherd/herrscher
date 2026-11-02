package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
)

func closeQuietly(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Debug("close", "err", err)
	}
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func mustReadFile(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustReadDir(t testing.TB, name string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(name)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}
