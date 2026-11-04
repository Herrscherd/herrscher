package conversation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLines(t *testing.T, home, dir, token string, lines ...string) {
	t.Helper()
	path := TranscriptPath(home, dir, token)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPositionIsTheLastMessageUUID(t *testing.T) {
	home, dir := t.TempDir(), "/work/tree"
	writeLines(t, home, dir, "tok", `{"uuid":"a","sessionId":"tok"}`, `{"type":"attachment"}`, `{"uuid":"b","sessionId":"tok"}`)

	got, err := Position(home, dir, "tok")
	if err != nil || got != "b" {
		t.Fatalf("Position = %q, %v; want b", got, err)
	}
}

func TestPositionWithoutTokenIsTheStart(t *testing.T) {
	got, err := Position(t.TempDir(), "/work/tree", "")
	if err != nil || got != Start {
		t.Fatalf("Position = %q, %v; want start", got, err)
	}
}

func TestPositionWithoutTranscriptIsUnsupported(t *testing.T) {
	_, err := Position(t.TempDir(), "/work/tree", "thread-of-another-backend")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a token with no Claude transcript must be unsupported, err = %v", err)
	}
}

func TestForkCopiesTheTranscriptUpToThePositionUnderANewID(t *testing.T) {
	home := t.TempDir()
	writeLines(t, home, "/src", "old", `{"uuid":"a","sessionId":"old"}`, `{"uuid":"b","sessionId":"old"}`, `{"uuid":"c","sessionId":"old"}`)

	token, err := Fork(home, "/src", "old", "b", "/dst", "new")
	if err != nil || token != "new" {
		t.Fatalf("Fork = %q, %v", token, err)
	}
	raw, err := os.ReadFile(TranscriptPath(home, "/dst", "new"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"uuid":"c"`) || !strings.Contains(string(raw), `"sessionId":"new"`) {
		t.Fatalf("forked transcript wrong: %s", raw)
	}
	original, err := os.ReadFile(TranscriptPath(home, "/src", "old"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(original), `"uuid":"c"`) {
		t.Fatal("the source transcript must be left whole")
	}
}

func TestForkToTheStartLeavesNoTranscript(t *testing.T) {
	token, err := Fork(t.TempDir(), "/src", "", Start, "/dst", "new")
	if err != nil || token != "" {
		t.Fatalf("Fork to the start = %q, %v; want no resume token", token, err)
	}
}

func TestNewTokenIsAUUIDAndUnique(t *testing.T) {
	first, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 36 || first == second || first[14] != '4' {
		t.Fatalf("tokens = %q, %q", first, second)
	}
}
