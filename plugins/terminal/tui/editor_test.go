package tui

import (
	"errors"
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
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
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
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("EDITOR", fakeEditor(t, "x"))
	m, _ := queueFixture(t)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if !m.editing {
		t.Fatal("ctrl+x ctrl+e must open the editor")
	}
}

func makeDraftRun(t *testing.T, root, name string, modified time.Time) string {
	t.Helper()
	dir := filepath.Join(root, draftRunPrefix+name)
	if err := os.Mkdir(dir, draftDirMode); err != nil {
		t.Fatal(err)
	}
	if _, err := writeDraftFile(dir, "x"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, modified, modified); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDraftRunsAreSweptOnlyWhenUnlockedAndOld(t *testing.T) {
	if !draftLocksSupported {
		t.Skip("this platform has no draft locks: the sweep only uses the age")
	}
	root := t.TempDir()
	old := time.Now().Add(-8 * 24 * time.Hour)
	lockedOld := makeDraftRun(t, root, "locked", old)
	held, err := lockRunDir(lockedOld)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := held.Close(); err != nil {
			t.Error(err)
		}
	})
	freeOld := makeDraftRun(t, root, "free", old)
	if free, err := lockRunDir(freeOld); err != nil {
		t.Fatal(err)
	} else if err := free.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(freeOld, old, old); err != nil {
		t.Fatal(err)
	}
	recent := makeDraftRun(t, root, "recent", time.Now())
	unrelated := filepath.Join(root, "notes")
	if err := os.MkdirAll(unrelated, draftDirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(unrelated, old, old); err != nil {
		t.Fatal(err)
	}
	if err := sweepDrafts(root); err != nil {
		t.Fatalf("sweepDrafts: %v", err)
	}
	if _, err := os.Stat(freeOld); !os.IsNotExist(err) {
		t.Fatal("an unlocked run older than the retention must be swept")
	}
	for _, kept := range []string{lockedOld, recent, unrelated} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("%s must survive the sweep: %v", filepath.Base(kept), err)
		}
	}
}

func TestOpenDraftRunRefusesAWideRoot(t *testing.T) {
	if !draftLocksSupported {
		t.Skip("this platform does not check the mode bits")
	}
	root := filepath.Join(t.TempDir(), "drafts")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := openDraftRun(root); !errors.Is(err, errUnsafeDraftDir) {
		t.Fatalf("a drafts root open to others must be refused, got %v", err)
	}
}

func TestOpenDraftRunCreatesALockedPrivateRun(t *testing.T) {
	root := filepath.Join(t.TempDir(), "drafts")
	run, err := openDraftRun(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := run.lock.Close(); err != nil {
			t.Error(err)
		}
	})
	if !strings.HasPrefix(filepath.Base(run.dir), draftRunPrefix) {
		t.Fatalf("run dir %q must carry the run prefix", run.dir)
	}
	if !draftLocksSupported {
		return
	}
	if !runDirHeld(run.dir) {
		t.Fatal("the run of a live process must hold its lock")
	}
	if info, err := os.Stat(run.dir); err != nil || info.Mode().Perm() != draftDirMode {
		t.Fatalf("run dir must be 0700, got %v (%v)", info, err)
	}
}
