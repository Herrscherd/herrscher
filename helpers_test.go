package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

var testLang = i18n.English

func testTranslator() *clitext.Translator {
	return clitext.For(testLang)
}

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
