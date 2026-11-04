package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func fakeEditor(t *testing.T, body string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '"+body+"' > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestEditorArgvPrefersEditorThenVisual(t *testing.T) {
	t.Setenv("EDITOR", "code -w")
	t.Setenv("VISUAL", "vim")
	if got := editorArgv("/tmp/x.md"); !slices.Equal(got, []string{"code", "-w", "/tmp/x.md"}) {
		t.Fatalf("argv = %v", got)
	}
	t.Setenv("EDITOR", "")
	if got := editorArgv("/tmp/x.md"); got[0] != "vim" {
		t.Fatalf("VISUAL must be the fallback: %v", got)
	}
	t.Setenv("VISUAL", "")
	if got := editorArgv("/tmp/x.md"); got[0] != fallbackEditor {
		t.Fatalf("vi must be the last fallback: %v", got)
	}
}

func TestOpenEditorMarksEditingAndSeedsFile(t *testing.T) {
	t.Setenv("EDITOR", fakeEditor(t, "x"))
	m, _ := queueFixture(t)
	typeLine(m, "brouillon initial")
	m.openEditor()
	if !m.editing {
		t.Fatal("opening the editor must mark the composer as editing")
	}
}

func TestEditorRunWritesBackIntoComposer(t *testing.T) {
	script := fakeEditor(t, "texte édité")
	t.Setenv("EDITOR", script)
	m, _ := queueFixture(t)
	path := filepath.Join(t.TempDir(), "draft.md")
	if err := os.WriteFile(path, []byte("brouillon\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.editing = true
	if err := editorCommand(t.Context(), path).Run(); err != nil {
		t.Fatalf("fake editor failed: %v", err)
	}
	m.applyEditorResult(editorDoneMsg{path: path})
	if m.editing {
		t.Fatal("applying the result must clear editing")
	}
	if m.input.Value() != "texte édité" {
		t.Fatalf("composer = %q", m.input.Value())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the temporary draft must be removed")
	}
}

func TestEditorErrorKeepsComposerAndSetsFlash(t *testing.T) {
	m, _ := queueFixture(t)
	typeLine(m, "intact")
	m.editing = true
	m.applyEditorResult(editorDoneMsg{path: filepath.Join(t.TempDir(), "absent.md"), err: os.ErrClosed})
	if m.input.Value() != "intact" || !strings.Contains(m.flash, "erreur") {
		t.Fatalf("editor error must keep the composer: input=%q flash=%q", m.input.Value(), m.flash)
	}
}

func TestCtrlXCtrlEOpensEditor(t *testing.T) {
	t.Setenv("EDITOR", fakeEditor(t, "x"))
	m, _ := queueFixture(t)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if !m.editing {
		t.Fatal("ctrl+x ctrl+e must open the editor")
	}
}

func TestStaleDraftsOfAKilledEditorAreSweptAtStartup(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	stale := filepath.Join(dir, "herrscher-draft-killed.md")
	fresh := filepath.Join(dir, "herrscher-draft-open.md")
	foreign := filepath.Join(dir, "notes.md")
	for path, age := range map[string]time.Duration{stale: 2 * draftMaxAge, fresh: time.Minute, foreign: 2 * draftMaxAge} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeStaleDrafts(dir, now); err != nil {
		t.Fatalf("removeStaleDrafts: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a draft left by a killed editor must be swept")
	}
	for _, kept := range []string{fresh, foreign} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("%s must survive the sweep: %v", filepath.Base(kept), err)
		}
	}
}
