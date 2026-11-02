package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func conversation(m *model) {
	tb := m.ensureTab("a")
	m.active = "a"
	tb.appendEntry(entry{role: roleYou, text: "first question"})
	tb.appendEntry(entry{role: roleAgent, text: "first answer"})
	tb.appendEntry(entry{role: roleYou, text: "second question"})
	tb.appendEntry(entry{role: roleTool, text: "bash(ls)"})
	tb.appendEntry(entry{role: roleThinking, text: "hmm"})
	tb.appendEntry(entry{role: roleAgent, text: "second answer"})
	tb.appendEntry(entry{role: roleCost, text: "$0.01"})
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
	if !strings.Contains(m.flash, "copié") {
		t.Fatalf("the copy must be acknowledged: %q", m.flash)
	}
}

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
	if m.flash == "" || strings.Contains(m.flash, "copié") {
		t.Fatalf("an empty copy must say why: %q", m.flash)
	}
}

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
	if !strings.Contains(m.statusBar(m.tabs[m.active], 120), "souris") {
		t.Fatalf("a released mouse must be named on the status bar: %q", m.statusBar(m.tabs[m.active], 120))
	}

	cmd = m.toggleMouse()
	if m.mouseFree {
		t.Fatal("the second toggle must take the mouse back")
	}
	if msg := runCmd(cmd); msg == nil {
		t.Fatal("taking the mouse back must emit the terminal mode change")
	}
	if strings.Contains(m.statusBar(m.tabs[m.active], 120), "souris") {
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
