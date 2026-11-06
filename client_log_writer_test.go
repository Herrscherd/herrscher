package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriterRotatesOnOverflowAndKeepsOneGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.log")
	writer, err := openRotatingWriter(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"aaaa\n", "bbbb\n", "cccc\n"} {
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path+".1", "aaaa\nbbbb\n")
	assertFile(t, path, "cccc\n")
}

func TestRotatingWriterBoundsEveryGenerationDuringAWriteRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.log")
	const limit = 20
	writer, err := openRotatingWriter(path, limit)
	if err != nil {
		t.Fatal(err)
	}
	for range 50 {
		if _, err := writer.Write([]byte("0123456\n")); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{path, path + ".1"} {
			info, statErr := os.Stat(name)
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			if statErr != nil || info.Size() > limit {
				t.Fatalf("%s = %v, %v; want at most %d bytes at every write", name, info, statErr, limit)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRotatingWriterReopensTheCurrentFileAtItsSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.log")
	if err := os.WriteFile(path, []byte("earlier\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := openRotatingWriter(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("later\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "earlier\nlater\n")
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatalf("a file under the cap must not rotate, stat .1 = %v", err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(want) {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
