package errwriter

import (
	"strings"
	"testing"
)

func TestPrintlnEscapesControlsAndKeepsTheLayout(t *testing.T) {
	var buf strings.Builder
	w := New(&buf)
	w.Println("ligne 1\n\tligne 2 \x1b[31mrouge\x1b[0m")
	got := buf.String()
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("output carries a raw ESC: %q", got)
	}
	if !strings.Contains(got, "ligne 1\n\tligne 2 ") {
		t.Fatalf("output = %q, want the line break and the tab kept", got)
	}
}

func TestPrintfEscapesBidiOverride(t *testing.T) {
	var buf strings.Builder
	w := New(&buf)
	w.Printf("%s\n", "fichier \u202egpj.exe")
	if strings.ContainsRune(buf.String(), '\u202e') {
		t.Fatalf("output carries a raw U+202E: %q", buf.String())
	}
}

func TestWriteStaysRaw(t *testing.T) {
	var buf strings.Builder
	w := New(&buf)
	if _, err := w.Write([]byte("jeton\x1b\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.String() != "jeton\x1b\n" {
		t.Fatalf("Write = %q, want the bytes unchanged", buf.String())
	}
}
