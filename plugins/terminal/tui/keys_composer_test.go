package tui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestComposerNewlineFollowsTheKeyTable(t *testing.T) {
	if keys := newTestModel().input.KeyMap.InsertNewline.Keys(); !slices.Contains(keys, "ctrl+j") {
		t.Fatalf("default newline keys = %v, want ctrl+j", keys)
	}
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	file := `{"bindings":[{"context":"Chat","bindings":{"ctrl+k":"chat:newline","ctrl+j":null}}]}`
	if err := os.WriteFile(filepath.Join(dir, keybindingsFile), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel()
	m.loadKeys(dir)
	keys := m.input.KeyMap.InsertNewline.Keys()
	if !slices.Contains(keys, "ctrl+k") {
		t.Fatalf("added chat:newline binding does not insert a newline: %v", keys)
	}
	if slices.Contains(keys, "ctrl+j") {
		t.Fatalf("null did not remove the default newline key: %v", keys)
	}
}

func TestNullOnAltEnterRemovesTheSteerAndTheNewline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	file := `{"bindings":[{"context":"Chat","bindings":{"alt+enter":null}}]}`
	if err := os.WriteFile(filepath.Join(dir, keybindingsFile), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeBackend{}
	m := readyModel(f)
	m.ensureTab("a")
	m.active = "a"
	m.loadKeys(dir)
	if keys := m.input.KeyMap.InsertNewline.Keys(); slices.Contains(keys, "alt+enter") {
		t.Fatalf("null on alt+enter must leave no newline key on it: %v", keys)
	}
	if chords := m.activeBindings().chordsOf(actQueueRedirect, ctxChat); len(chords) != 0 {
		t.Fatalf("null on alt+enter must leave chat:queueRedirect without a chord: %v", chords)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ligne")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if got := m.input.Value(); got != "ligne" {
		t.Fatalf("alt+enter unbound must insert nothing: value=%q", got)
	}
	m.tabs["a"].busy = true
	m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if len(m.tabs["a"].queue) != 0 {
		t.Fatalf("alt+enter unbound must not queue a steer: %+v", m.tabs["a"].queue)
	}
}
