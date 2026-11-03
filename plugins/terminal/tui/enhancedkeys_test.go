package tui

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestEnableEnhancedKeysRequestsTheProtocol(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	msg := enableEnhancedKeys()
	os.Stdout = saved
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != enhancedKeysOn {
		t.Fatalf("wrote %q, want %q", got, enhancedKeysOn)
	}
	if msg != nil {
		t.Fatalf("a terminal request must produce no message; got %#v", msg)
	}
}

func TestInitAsksForTheProtocolOnTheAltScreen(t *testing.T) {
	src, err := os.ReadFile("tui.go")
	if err != nil {
		t.Fatal(err)
	}
	init := string(src)
	i := strings.Index(init, "func (m *model) Init()")
	if i < 0 {
		t.Fatal("Init not found")
	}
	end := strings.Index(init[i:], "\n}")
	if end < 0 || !strings.Contains(init[i:i+end], "enableEnhancedKeys") {
		t.Error("Init must ask for the enhanced keyboard protocol on the alternate screen")
	}
}
