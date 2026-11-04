package tui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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
