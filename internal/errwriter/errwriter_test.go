package errwriter

import (
	"bytes"
	"errors"
	"testing"
)

var errSink = errors.New("sink closed")

type failingSink struct{ wrote int }

func (f *failingSink) Write([]byte) (int, error) {
	f.wrote++
	return 0, errSink
}

func TestFirstWriteErrorSticksAndLaterWritesAreSkipped(t *testing.T) {
	sink := &failingSink{}
	w := New(sink)
	w.Println("one")
	w.Printf("%s\n", "two")
	w.Println("three")
	if !errors.Is(w.Err(), errSink) {
		t.Fatalf("Err() = %v, want %v", w.Err(), errSink)
	}
	if sink.wrote != 1 {
		t.Fatalf("writes after the first failure reached the sink: %d", sink.wrote)
	}
}

func TestFinishTurnsStdoutFailureIntoExitOne(t *testing.T) {
	var stderr bytes.Buffer
	out := New(&failingSink{})
	errOut := New(&stderr)
	out.Println("lost")
	if code := Finish(0, out, errOut); code != 1 {
		t.Fatalf("Finish(0) = %d, want 1", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("the stdout failure was not reported on stderr")
	}
}

func TestFinishKeepsAnExistingFailureCode(t *testing.T) {
	out := New(&bytes.Buffer{})
	errOut := New(&bytes.Buffer{})
	out.Println("fine")
	if code := Finish(2, out, errOut); code != 2 {
		t.Fatalf("Finish(2) = %d, want 2", code)
	}
}
