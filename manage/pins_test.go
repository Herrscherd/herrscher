package manage

import (
	"strings"
	"testing"
)

func TestPinsReadEmpty(t *testing.T) {
	pins, err := readPins("")
	if err != nil {
		t.Fatalf("readPins: %v", err)
	}
	if len(pins) != 0 {
		t.Fatalf("want no pins, got %v", pins)
	}
}

func TestPinsReadModules(t *testing.T) {
	pins, err := readPins("mod/a\nmod/b\n")
	if err != nil {
		t.Fatalf("readPins: %v", err)
	}
	if !pins["mod/a"] || !pins["mod/b"] || len(pins) != 2 {
		t.Fatalf("want mod/a and mod/b, got %v", pins)
	}
}

func TestPinsReadSkipsCommentsAndBlanks(t *testing.T) {
	pins, err := readPins("# note\n\nmod/a\n")
	if err != nil {
		t.Fatalf("readPins: %v", err)
	}
	if len(pins) != 1 || !pins["mod/a"] {
		t.Fatalf("want only mod/a, got %v", pins)
	}
}

func TestPinsReadTrimsSpaces(t *testing.T) {
	pins, err := readPins("   mod/a   \n\t# indented comment\n")
	if err != nil {
		t.Fatalf("readPins: %v", err)
	}
	if len(pins) != 1 || !pins["mod/a"] {
		t.Fatalf("want only mod/a, got %v", pins)
	}
}

func TestPinsReadRejectsMalformedLine(t *testing.T) {
	_, err := readPins("mod/a\nnot a path\n")
	if err == nil {
		t.Fatal("want an error naming the bad line")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Fatalf("error should name line 2, got %q", err)
	}
}

func TestPinsRoundTrip(t *testing.T) {
	pins, err := readPins("# note\nmod/b\nmod/a\n")
	if err != nil {
		t.Fatalf("readPins: %v", err)
	}
	if got, want := writePins(pins), "mod/a\nmod/b\n"; got != want {
		t.Fatalf("writePins = %q, want %q", got, want)
	}
}

func TestPinsWriteEmpty(t *testing.T) {
	if got := writePins(nil); got != "" {
		t.Fatalf("writePins(nil) = %q, want empty", got)
	}
}
