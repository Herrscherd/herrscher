package tui

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
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
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close writer: %v", closeErr)
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

func pressN(m *model) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
}

func TestNKeyCyclesWhileToastIsVisible(t *testing.T) {
	m := notifyModel(t, false)
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"api"})
	pressN(m)
	if m.notifyMode != notifyTurnEnd {
		t.Fatalf("n with a toast on screen must cycle to turn end, got mode %d", m.notifyMode)
	}
	if m.toast.title == "" {
		t.Fatal("cycling notifications must not dismiss the toast")
	}
}

func TestNKeyCyclesNotificationsInContextOverlay(t *testing.T) {
	m := notifyModel(t, false)
	m.openContext()
	pressN(m)
	if m.notifyMode != notifyTurnEnd {
		t.Fatalf("n in the /context overlay must cycle notifications, got mode %d", m.notifyMode)
	}
}

func TestNKeyIsTypedWithoutToast(t *testing.T) {
	m := notifyModel(t, false)
	pressN(m)
	if m.input.Value() != "n" {
		t.Fatalf("n without a toast must be typed in the composer, got %q", m.input.Value())
	}
	if m.notifyMode != notifyBoth {
		t.Fatal("n without a toast must not change the notification mode")
	}
}

func TestNKeyIsTypedWhenComposerHasText(t *testing.T) {
	m := notifyModel(t, false)
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"api"})
	m.input.SetValue("bonjour")
	pressN(m)
	if m.notifyMode != notifyBoth {
		t.Fatal("n must not toggle notifications while the composer holds text")
	}
}

func TestCtrlPJumpsToTheNotifyingSessionWhileToastShows(t *testing.T) {
	m := notifyModel(t, false)
	m.ensureTab("a")
	m.active = "a"
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: "reply", Done: true}})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.active != "c" {
		t.Fatalf("ctrl+p with a toast must go to the notifying session, active = %q", m.active)
	}
	if m.toast.title != "" {
		t.Fatal("jumping to the session must clear the toast")
	}
}

func TestApprovalToastJumpsToItsSession(t *testing.T) {
	m := notifyModel(t, false)
	m.ensureTab("a")
	m.active = "c"
	m.notifyApproval("a", "run Bash(npm run deploy)")
	if !strings.Contains(strings.Join(m.toast.body, " "), "run Bash") {
		t.Fatalf("the approval toast must name what waits, got %q", m.toast.body)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.active != "a" {
		t.Fatalf("ctrl+p must go to the session asking for approval, active = %q", m.active)
	}
}

func TestCtrlPWithoutToastKeepsItsRole(t *testing.T) {
	m := notifyModel(t, false)
	m.ensureTab("a")
	m.active = "c"
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.active != "c" {
		t.Fatalf("ctrl+p without a toast must not change the tab, active = %q", m.active)
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
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"a"})
	first := m.toast.seq
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"b"})
	m.Update(toastExpiredMsg{seq: first})
	if m.toast.title == "" {
		t.Fatal("an older timer must not remove a newer toast")
	}
}

func TestToastShowsAgeDeliveryAndJumpHint(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := notifyModel(t, true)
		m.handleWindowSize(tea.WindowSizeMsg{Width: width, Height: 20})
		m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"api"})
		view := ansi.Strip(m.View())
		for _, want := range []string{"il y a 0s", "OSC 9 envoyé", "⌃P y aller"} {
			if !strings.Contains(view, want) {
				t.Fatalf("width %d: the toast must show %q, window:\n%s", width, want, view)
			}
		}
	}
}

func TestTurnEndToastWithoutStartTimeDoesNotPanic(t *testing.T) {
	m := notifyModel(t, false)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: "reply", Done: true}})
	if !strings.Contains(ansi.Strip(m.View()), "Tour fini") {
		t.Fatal("a turn without a start time must still raise the toast")
	}
}

func TestToastFitsAnyWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := notifyModel(t, false)
		m.handleWindowSize(tea.WindowSizeMsg{Width: width, Height: 20})
		m.announce(kindApproval, unicodeGlyphs().attention, "Approbation en attente", "c", []string{strings.Repeat("Bash npm run deploy ", 12)})
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

func TestToastAgeAdvancesOnEachTick(t *testing.T) {
	m := notifyModel(t, false)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"api"})
	m.toast.at = time.Now().Add(-3 * time.Second)
	if !strings.Contains(ansi.Strip(m.View()), "il y a 3s") {
		t.Fatal("the toast must show the age of its event")
	}
	m.toast.at = time.Now().Add(-4 * time.Second)
	_, cmd := m.Update(toastTickMsg{seq: m.toast.seq})
	if cmd == nil {
		t.Fatal("a tick while the toast shows must schedule the next tick")
	}
	if !strings.Contains(ansi.Strip(m.View()), "il y a 4s") {
		t.Fatal("the age must advance when a tick arrives")
	}
}

func TestToastTickStopsWhenTheToastIsGone(t *testing.T) {
	m := notifyModel(t, false)
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"api"})
	seq := m.toast.seq
	m.Update(toastExpiredMsg{seq: seq})
	if _, cmd := m.Update(toastTickMsg{seq: seq}); cmd != nil {
		t.Fatal("a tick for a vanished toast must not be rescheduled")
	}
}

func TestComposerPromptStaysAngleBracketInShellMode(t *testing.T) {
	m := notifyModel(t, false)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.input.SetValue("!ls")
	m.buildFrame()
	if got := ansi.Strip(m.inputRow()); !strings.HasPrefix(got, "> !ls") {
		t.Fatalf("shell mode keeps the > prompt, got %q", got)
	}
	m.input.SetValue("ls")
	m.buildFrame()
	if got := ansi.Strip(m.inputRow()); !strings.HasPrefix(got, "> ls") {
		t.Fatalf("outside shell mode the prompt must be >, got %q", got)
	}
}
