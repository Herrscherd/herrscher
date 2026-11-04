package tui

import (
	"io"
	"os"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = saved }()
	run()
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	return string(out)
}

func notifyModel(t *testing.T, osc9 bool) *model {
	t.Helper()
	m := newModel(&fakeBackend{})
	m.caps.OSC9 = osc9
	m.ensureTab("c")
	m.active = "c"
	return m
}

func TestOSC9IsWrittenWhenWindowLacksFocus(t *testing.T) {
	m := notifyModel(t, true)
	m.Update(tea.BlurMsg{})
	cmd := m.signalCmd("Tour fini")
	out := captureStdout(t, func() { runCmd(cmd) })
	if out != "\x1b]9;Tour fini\x07" {
		t.Fatalf("written %q, want an OSC 9 notification", out)
	}
}

func TestBELIsWrittenWhenTerminalIgnoresOSC9(t *testing.T) {
	m := notifyModel(t, false)
	m.Update(tea.BlurMsg{})
	out := captureStdout(t, func() { runCmd(m.signalCmd("Tour fini")) })
	if out != "\a" {
		t.Fatalf("written %q, want the BEL fallback", out)
	}
}

func TestNothingIsWrittenWhileWindowHasFocus(t *testing.T) {
	m := notifyModel(t, true)
	m.Update(tea.FocusMsg{})
	if cmd := m.signalCmd("Tour fini"); cmd != nil {
		out := captureStdout(t, func() { runCmd(cmd) })
		t.Fatalf("a focused window must not be signalled, wrote %q", out)
	}
}

func TestSignalsAlwaysWhenFocusIsNotReported(t *testing.T) {
	m := notifyModel(t, true)
	out := captureStdout(t, func() { runCmd(m.signalCmd("Tour fini")) })
	if !strings.Contains(out, "\x1b]9;") {
		t.Fatalf("without focus reports the notification must be written, got %q", out)
	}
}

func TestOSC9TextCannotInjectControlSequences(t *testing.T) {
	m := notifyModel(t, true)
	m.Update(tea.BlurMsg{})
	out := captureStdout(t, func() { runCmd(m.signalCmd("a\x07b\x1b[2Jc")) })
	if out != "\x1b]9;ab[2Jc\x07" {
		t.Fatalf("written %q, want the control characters stripped", out)
	}
}

func TestNKeyCyclesWhichNotificationsAreActive(t *testing.T) {
	m := notifyModel(t, false)
	press := func() { m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}) }

	press()
	m.notifyApproval("run Bash")
	if m.toast.title != "" {
		t.Fatalf("turn-end mode must not show an approval toast, got %q", m.toast.title)
	}
	press()
	m.notifyTurnEnd(m.tabs["c"])
	if m.toast.title != "" {
		t.Fatalf("approval mode must not show a turn-end toast, got %q", m.toast.title)
	}
	press()
	m.notifyApproval("run Bash")
	if m.toast.title == "" {
		t.Fatal("back to both kinds, the approval toast must show")
	}
}

func TestNKeyIsTypedWhenComposerHasText(t *testing.T) {
	m := notifyModel(t, false)
	m.input.SetValue("bonjour")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if m.notifyMode != notifyBoth {
		t.Fatal("n must not toggle notifications while the composer holds text")
	}
}

func TestTurnEndToastAppearsAndExpires(t *testing.T) {
	m := notifyModel(t, false)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: "reply", Done: true}})
	if !strings.Contains(ansi.Strip(m.View()), "Tour fini") {
		t.Fatal("the turn-end toast must show in the window")
	}
	m.Update(toastExpiredMsg{seq: m.toast.seq})
	if strings.Contains(ansi.Strip(m.View()), "Tour fini") {
		t.Fatal("the toast must disappear when its timer fires")
	}
}

func TestStaleToastTimerKeepsNewerToast(t *testing.T) {
	m := notifyModel(t, false)
	m.announce(kindTurnEnd, glyphDone, "Tour fini", "a")
	first := m.toast.seq
	m.announce(kindTurnEnd, glyphDone, "Tour fini", "b")
	m.Update(toastExpiredMsg{seq: first})
	if m.toast.title == "" {
		t.Fatal("an older timer must not remove a newer toast")
	}
}

func TestToastFitsAnyWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := notifyModel(t, false)
		m.handleWindowSize(tea.WindowSizeMsg{Width: width, Height: 24})
		m.announce(kindApproval, glyphWaiting, "Approbation en attente", strings.Repeat("Bash npm run deploy ", 12))
		for _, line := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d: line of %d cells overflows: %q", width, lipgloss.Width(line), ansi.Strip(line))
			}
		}
	}
}

func TestTerminalsThatHandleOSC9AreProbed(t *testing.T) {
	cases := map[string]bool{"iTerm.app": true, "WezTerm": true, "ghostty": true, "Apple_Terminal": false, "": false}
	for program, want := range cases {
		caps := Probe(func(key string) string {
			if key == "TERM_PROGRAM" {
				return program
			}
			return ""
		})
		if caps.OSC9 != want {
			t.Fatalf("TERM_PROGRAM=%q: OSC9 = %v, want %v", program, caps.OSC9, want)
		}
	}
}

func TestFocusReportsPassThroughTheInputFilter(t *testing.T) {
	focusIn, focusOut := "\x1b[I", "\x1b[O"
	if got := string(filter([]byte(focusIn + focusOut))); got != focusIn+focusOut {
		t.Fatalf("focus reports rewritten to %q, must reach bubbletea unchanged", got)
	}
}
