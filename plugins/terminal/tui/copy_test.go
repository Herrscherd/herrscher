package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// conversation seeds a tab with an exchange plus the lines this window adds
// around it, so every test here also asserts what copying leaves out.
func conversation(m *model) *tab {
	tb := m.ensureTab("a")
	m.active = "a"
	tb.appendEntry(entry{role: roleYou, text: "first question"})
	tb.appendEntry(entry{role: roleAgent, text: "first answer"})
	tb.appendEntry(entry{role: roleYou, text: "second question"})
	tb.appendEntry(entry{role: roleTool, text: "bash(ls)"})
	tb.appendEntry(entry{role: roleThinking, text: "hmm"})
	tb.appendEntry(entry{role: roleAgent, text: "second answer"})
	tb.appendEntry(entry{role: roleCost, text: "$0.01"})
	return tb
}

func TestCopyTakesTheLastAnswerByDefault(t *testing.T) {
	m := newTestModel()
	clip := &fakeWriteClipboard{}
	m.clip = clip
	conversation(m)

	m.copyCmd(nil)
	if clip.wrote != "second answer" {
		t.Fatalf("/copy must take the last answer alone, got %q", clip.wrote)
	}
	if !strings.Contains(m.flash, "copied") {
		t.Fatalf("the copy must be acknowledged: %q", m.flash)
	}
}

// A turn is a question and its answer: pasting the answer without what was
// asked is the half that needs the other half to mean anything.
func TestCopyTurnCarriesTheQuestionWithTheAnswer(t *testing.T) {
	m := newTestModel()
	clip := &fakeWriteClipboard{}
	m.clip = clip
	conversation(m)

	m.copyCmd([]string{"turn"})
	if !strings.Contains(clip.wrote, "second question") || !strings.Contains(clip.wrote, "second answer") {
		t.Fatalf("a turn must hold both sides: %q", clip.wrote)
	}
	if strings.Contains(clip.wrote, "first question") {
		t.Fatalf("a turn stops at the last thing you sent: %q", clip.wrote)
	}
}

func TestCopyAllTakesTheWholeTranscript(t *testing.T) {
	m := newTestModel()
	clip := &fakeWriteClipboard{}
	m.clip = clip
	conversation(m)

	m.copyCmd([]string{"all"})
	for _, want := range []string{"first question", "first answer", "second question", "second answer"} {
		if !strings.Contains(clip.wrote, want) {
			t.Fatalf("/copy all dropped %q: %q", want, clip.wrote)
		}
	}
}

// The window's own commentary is for the screen. A paste of it into an issue is
// noise the reader steps over.
func TestCopyLeavesOutWhatTheWindowSaid(t *testing.T) {
	m := newTestModel()
	clip := &fakeWriteClipboard{}
	m.clip = clip
	conversation(m)

	m.copyCmd([]string{"all"})
	for _, unwanted := range []string{"bash(ls)", "hmm", "$0.01"} {
		if strings.Contains(clip.wrote, unwanted) {
			t.Fatalf("copy must leave out %q: %q", unwanted, clip.wrote)
		}
	}
}

// A typo copies the wrong thing, and that is discovered at the paste — too far
// from here to connect it back. So refuse by name instead.
func TestCopyRefusesAnUnknownTarget(t *testing.T) {
	m := newTestModel()
	clip := &fakeWriteClipboard{}
	m.clip = clip
	conversation(m)

	m.copyCmd([]string{"evertything"})
	if clip.wrote != "" {
		t.Fatalf("an unknown target must write nothing, got %q", clip.wrote)
	}
	for _, target := range copyTargets {
		if !strings.Contains(m.flash, target) {
			t.Fatalf("the refusal must name %q: %q", target, m.flash)
		}
	}
}

func TestCopyOnAnEmptyTabSaysSo(t *testing.T) {
	m := newTestModel()
	clip := &fakeWriteClipboard{}
	m.clip = clip
	m.ensureTab("a")
	m.active = "a"

	m.copyCmd(nil)
	if clip.wrote != "" {
		t.Fatalf("nothing to copy must write nothing, got %q", clip.wrote)
	}
	if m.flash == "" || strings.Contains(m.flash, "copied") {
		t.Fatalf("an empty copy must say why: %q", m.flash)
	}
}

// alt+y is the shortcut for the common case, and has to reach the same place
// /copy does.
func TestAltYCopiesTheLastAnswer(t *testing.T) {
	m := newTestModel()
	clip := &fakeWriteClipboard{}
	m.clip = clip
	conversation(m)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y"), Alt: true})
	if clip.wrote != "second answer" {
		t.Fatalf("alt+y must copy the last answer, got %q", clip.wrote)
	}
}

// A plain "y" is a character being typed, not a shortcut.
func TestPlainYIsTypedNotCopied(t *testing.T) {
	m := newTestModel()
	clip := &fakeWriteClipboard{}
	m.clip = clip
	conversation(m)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if clip.wrote != "" {
		t.Fatalf("a typed y must not copy: %q", clip.wrote)
	}
}

// The state and the escape sequence are set together, so they cannot disagree
// about who holds the mouse.
func TestToggleMouseHandsTheMouseOverAndTakesItBack(t *testing.T) {
	m := newTestModel()
	conversation(m)

	cmd := m.toggleMouse()
	if !m.mouseFree {
		t.Fatal("the first toggle must release the mouse")
	}
	if msg := runCmd(cmd); msg == nil {
		t.Fatal("releasing the mouse must emit the terminal mode change")
	}
	if !strings.Contains(m.statusBar(m.tabs[m.active], 120), "mouse") {
		t.Fatalf("a released mouse must be named on the status bar: %q", m.statusBar(m.tabs[m.active], 120))
	}

	cmd = m.toggleMouse()
	if m.mouseFree {
		t.Fatal("the second toggle must take the mouse back")
	}
	if msg := runCmd(cmd); msg == nil {
		t.Fatal("taking the mouse back must emit the terminal mode change")
	}
	if strings.Contains(m.statusBar(m.tabs[m.active], 120), "mouse") {
		t.Fatal("the default capture needs no announcement")
	}
}

func TestCtrlGTogglesTheMouse(t *testing.T) {
	m := newTestModel()
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if !m.mouseFree {
		t.Fatal("ctrl+g must release the mouse")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if m.mouseFree {
		t.Fatal("ctrl+g must take the mouse back")
	}
}
