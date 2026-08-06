package tui

import (
	"io"
	"os"
	"strings"
	"testing"
)

// TestEnableEnhancedKeysRequestsTheProtocol: the whole Shift+Enter translation
// downstream is dead weight if the request never leaves.
func TestEnableEnhancedKeysRequestsTheProtocol(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	msg := enableEnhancedKeys()
	os.Stdout = saved
	w.Close()

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

// TestInitAsksForTheProtocolOnTheAltScreen guards the ordering bug this fixed:
// the pre-program push lands on the main screen, and the alternate screen the
// TUI runs on keeps its own stack — so without a second push from Init,
// Shift+Enter arrives as a bare CR on terminals that support the protocol fully.
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
