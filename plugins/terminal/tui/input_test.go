package tui

import (
	"bytes"
	"os"
	"testing"
)

// filter runs the state machine over the given chunks (each chunk simulates one
// underlying tty Read) and returns the surviving bytes.
func filter(chunks ...[]byte) []byte {
	f := &filteredStdin{}
	for _, c := range chunks {
		f.feed(c)
	}
	return f.out
}

func TestFilterDropsOSCColorResponse(t *testing.T) {
	// The exact leak from the bug report: an OSC 11 background-colour reply,
	// ST-terminated, arriving ahead of real typing.
	in := []byte("\x1b]11;rgb:1a1a/1a1a/1a1a\x1b\\hello")
	if got := filter(in); !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("OSC not stripped: got %q", got)
	}
}

func TestFilterDropsOSCResponseBELTerminated(t *testing.T) {
	in := []byte("\x1b]11;rgb:2020/2020/2020\ahi")
	if got := filter(in); !bytes.Equal(got, []byte("hi")) {
		t.Fatalf("BEL-terminated OSC not stripped: got %q", got)
	}
}

func TestFilterDropsCursorPositionReport(t *testing.T) {
	in := []byte("\x1b[24;80Rx")
	if got := filter(in); !bytes.Equal(got, []byte("x")) {
		t.Fatalf("cursor report not stripped: got %q", got)
	}
}

func TestFilterKeepsRealKeys(t *testing.T) {
	// A lone Esc keypress (trailing ESC, no following byte in the chunk) and an
	// arrow key (CSI ending in a non-R final byte) must both survive.
	cases := map[string][]byte{
		"lone esc":     []byte("\x1b"),
		"up arrow":     []byte("\x1b[A"),
		"esc then key": []byte("\x1babc"),
		"plain text":   []byte("hello world"),
	}
	for name, in := range cases {
		if got := filter(in); !bytes.Equal(got, in) {
			t.Fatalf("%s: real input altered: got %q want %q", name, got, in)
		}
	}
}

func TestFilterHandlesSplitOSCAcrossReads(t *testing.T) {
	// The colour reply is split across two underlying reads; the machine must
	// stay in the OSC state between chunks and still drop all of it.
	got := filter([]byte("\x1b]11;rgb:1a1a/1a"), []byte("1a/1a1a\x1b\\done"))
	if !bytes.Equal(got, []byte("done")) {
		t.Fatalf("split OSC not stripped: got %q", got)
	}
}

func TestFilterTrailingEscIsRealKey(t *testing.T) {
	// An ESC that is the last byte of a chunk is a genuine Esc keypress: it must
	// pass through immediately, not be withheld waiting for a classifier byte.
	if got := filter([]byte("ab\x1b")); !bytes.Equal(got, []byte("ab\x1b")) {
		t.Fatalf("trailing ESC withheld: got %q", got)
	}
}

// A chunk that filters to nothing must make Read return (0, nil) — not loop and
// block on a second underlying read — so shutdown stays cancelable. A later real
// key must then read back intact.
func TestReadReturnsZeroOnFullyDroppedChunk(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	f := &filteredStdin{File: r}

	if _, err := w.Write([]byte("\x1b]11;rgb:1a1a/1a1a/1a1a\x1b\\")); err != nil {
		t.Fatalf("write osc: %v", err)
	}
	buf := make([]byte, 64)
	n, err := f.Read(buf)
	if n != 0 || err != nil {
		t.Fatalf("dropped chunk: got n=%d err=%v, want 0,nil", n, err)
	}

	if _, err := w.Write([]byte("k")); err != nil {
		t.Fatalf("write key: %v", err)
	}
	n, err = f.Read(buf)
	if err != nil || string(buf[:n]) != "k" {
		t.Fatalf("real key after drop: got %q err=%v", buf[:n], err)
	}
}

func TestFilterMalformedCSIReplayed(t *testing.T) {
	// A CSI that never reaches a final byte within the cap must be replayed, not
	// swallowed, so input is never lost to a stray ESC-[.
	in := append([]byte("\x1b["), bytes.Repeat([]byte("9"), csiSeqCap+5)...)
	got := filter(in)
	if !bytes.Equal(got, in) {
		t.Fatalf("malformed CSI not replayed: got %q", got)
	}
}
