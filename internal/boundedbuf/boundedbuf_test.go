package boundedbuf

import (
	"bytes"
	"strings"
	"testing"
)

func TestBufferKeepsTheHeadAndDrainsTheRest(t *testing.T) {
	buf := New(8)
	chunk := []byte(strings.Repeat("x", 1<<20))
	for range 4 {
		n, err := buf.Write(chunk)
		if err != nil || n != len(chunk) {
			t.Fatalf("Write = %d, %v; the writer must accept everything so the child never blocks", n, err)
		}
	}
	if got := buf.String(); got != "xxxxxxxx" {
		t.Fatalf("String = %q, want the first 8 bytes", got)
	}
	if !buf.Truncated() {
		t.Fatal("Truncated must report the discarded excess")
	}
}

func TestBufferUnderTheLimitIsNotTruncated(t *testing.T) {
	buf := New(16)
	var sink bytes.Buffer
	sink.WriteString("court")
	if _, err := buf.Write(sink.Bytes()); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "court" || buf.Truncated() {
		t.Fatalf("String = %q, Truncated = %v", buf.String(), buf.Truncated())
	}
}

func TestBufferKeepsTheHeadAcrossSmallWrites(t *testing.T) {
	buf := New(5)
	for _, piece := range []string{"ab", "cd", "ef"} {
		if _, err := buf.Write([]byte(piece)); err != nil {
			t.Fatal(err)
		}
	}
	if buf.String() != "abcde" || !buf.Truncated() {
		t.Fatalf("String = %q, Truncated = %v", buf.String(), buf.Truncated())
	}
}
