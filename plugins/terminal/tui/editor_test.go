package tui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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

func holdDraftLock(t *testing.T, dir string) *os.File {
	t.Helper()
	runRoot, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	lock, lockErr := acquireDraftLock(runRoot)
	if err := runRoot.Close(); err != nil {
		t.Error(err)
	}
	if lockErr != nil {
		t.Fatal(lockErr)
	}
	t.Cleanup(func() {
		if lock == nil {
			return
		}
		if err := lock.Close(); err != nil {
			t.Error(err)
		}
	})
	return lock
}

func ageLock(t *testing.T, dir string, modified time.Time) {
	t.Helper()
	lock := filepath.Join(dir, draftLockName)
	if err := os.WriteFile(lock, nil, draftLockMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(lock, modified, modified); err != nil {
		t.Fatal(err)
	}
}

func TestDraftRunsAreSweptOnlyWhenUnlockedAndOld(t *testing.T) {
	if !draftLocksSupported {
		t.Skip("this platform has no draft locks: the sweep only uses the age")
	}
	root := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	lockedOld := makeDraftRun(t, root, "locked", old)
	holdDraftLock(t, lockedOld)
	freeOld := makeDraftRun(t, root, "free", old)
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

func TestRunWithAFreshLockIsNeverSwept(t *testing.T) {
	root := t.TempDir()
	ancient := time.Now().Add(-8 * 24 * time.Hour)
	lapsed := time.Now().Add(-48 * time.Hour)
	live := makeDraftRun(t, root, "live", ancient)
	ageLock(t, live, time.Now())
	stale := makeDraftRun(t, root, "stale", lapsed)
	ageLock(t, stale, lapsed)
	if err := os.Chtimes(live, ancient, ancient); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, lapsed, lapsed); err != nil {
		t.Fatal(err)
	}
	if err := sweepDrafts(root); err != nil {
		t.Fatalf("sweepDrafts: %v", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("a run whose lock was refreshed recently must survive: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a run whose lock is older than the retention must be swept")
	}
}

func TestHeartbeatKeepsALiveRunsLockFresh(t *testing.T) {
	run, err := startDraftRun(t.Context(), filepath.Join(t.TempDir(), "drafts"), 5*time.Millisecond, touchDraftLock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := run.release(); err != nil {
			t.Error(err)
		}
	})
	lock := filepath.Join(run.dir, draftLockName)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, err := os.Stat(lock)
		if err != nil {
			t.Fatal(err)
		}
		if time.Since(info.ModTime()) < time.Hour {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the live run must refresh its lock on its own")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSweepNeverFollowsALinkOutOfTheRoot(t *testing.T) {
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.md")
	if err := os.WriteFile(keep, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, draftRunPrefix+"link")); err != nil {
		t.Skip("symlinks unavailable: " + err.Error())
	}
	if err := sweepDrafts(root); err != nil {
		t.Fatalf("sweepDrafts: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("a link out of the drafts root must never be followed: %v", err)
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
	if _, err := openDraftRun(t.Context(), root); !errors.Is(err, errUnsafeDraftDir) {
		t.Fatalf("a drafts root open to others must be refused, got %v", err)
	}
}

func TestOpenDraftRunCreatesALockedPrivateRun(t *testing.T) {
	root := filepath.Join(t.TempDir(), "drafts")
	run, err := openDraftRun(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := run.release(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	if !strings.HasPrefix(filepath.Base(run.dir), draftRunPrefix) {
		t.Fatalf("run dir %q must carry the run prefix", run.dir)
	}
	if info, statErr := os.Stat(run.dir); statErr != nil || info.Mode().Perm() != draftDirMode {
		t.Fatalf("run dir must be 0700, got %v (%v)", info, statErr)
	}
	if _, statErr := os.Stat(filepath.Join(run.dir, draftLockName)); statErr != nil {
		t.Fatalf("the run must own its lock file: %v", statErr)
	}
	if !draftLocksSupported {
		return
	}
	parent, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := parent.Close(); err != nil {
			t.Error(err)
		}
	}()
	if !runDirHeld(parent, filepath.Base(run.dir)) {
		t.Fatal("the run of a live process must hold its lock")
	}
}

func TestHeartbeatSurvivesATransientFailure(t *testing.T) {
	failedOnce := false
	touch := func(root *os.Root) error {
		if !failedOnce {
			failedOnce = true
			return errors.New("toucher transitoire")
		}
		return touchDraftLock(root)
	}
	run, err := startDraftRun(t.Context(), filepath.Join(t.TempDir(), "drafts"), 5*time.Millisecond, touch)
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(run.dir, draftLockName)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, err := os.Stat(lock)
		if err != nil {
			t.Fatal(err)
		}
		if time.Since(info.ModTime()) < time.Hour {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a transient failure must not stop the heartbeat: the next tick must refresh the lock")
		}
		time.Sleep(time.Millisecond)
	}
	if err := run.release(); err != nil {
		t.Fatalf("release after a recovered beat must be nil, got %v", err)
	}
}

func TestReleaseDraftsTwiceIsSafe(t *testing.T) {
	run, err := openDraftRun(t.Context(), filepath.Join(t.TempDir(), "drafts"))
	if err != nil {
		t.Fatal(err)
	}
	m := newTestModel()
	m.draftFiles = run
	if err := m.releaseDrafts(); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if err := m.releaseDrafts(); err != nil {
		t.Fatalf("second release must be a no-op, got %v", err)
	}
}

func TestReleaseReportsNoBeatErrorAfterARecovery(t *testing.T) {
	var calls atomic.Int32
	touch := func(root *os.Root) error {
		if calls.Add(1) == 1 {
			return errors.New("panne transitoire")
		}
		return touchDraftLock(root)
	}
	run, err := startDraftRun(t.Context(), filepath.Join(t.TempDir(), "drafts"), 5*time.Millisecond, touch)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := run.release(); err != nil {
		t.Fatalf("a beat that recovered must not fail the release, got %v", err)
	}
}

func TestReleaseReportsAFailureAtTheLastBeat(t *testing.T) {
	var calls atomic.Int32
	failing := make(chan struct{})
	var signal sync.Once
	touch := func(root *os.Root) error {
		if calls.Add(1) < 3 {
			return touchDraftLock(root)
		}
		signal.Do(func() { close(failing) })
		return errors.New("panne finale")
	}
	run, err := startDraftRun(t.Context(), filepath.Join(t.TempDir(), "drafts"), 5*time.Millisecond, touch)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-failing:
	case <-time.After(5 * time.Second):
		t.Fatal("the failing beat never ran")
	}
	if err := run.release(); err == nil || !strings.Contains(err.Error(), "panne finale") {
		t.Fatalf("release must report the failure of the last beat, got %v", err)
	}
}

func TestDraftRunReleaseIsIdempotent(t *testing.T) {
	run, err := startDraftRun(t.Context(), filepath.Join(t.TempDir(), "drafts"), time.Hour, touchDraftLock)
	if err != nil {
		t.Fatal(err)
	}
	if err := run.release(); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if err := run.release(); err != nil {
		t.Fatalf("second direct release must be a no-op, got %v", err)
	}
}

func TestBeatFailureFlashesOnceAndDoesNotLoop(t *testing.T) {
	var calls atomic.Int32
	touch := func(root *os.Root) error {
		if calls.Add(1) == 1 {
			return errors.New("panne durable")
		}
		return touchDraftLock(root)
	}
	run, err := startDraftRun(t.Context(), filepath.Join(t.TempDir(), "drafts"), 5*time.Millisecond, touch)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := run.release(); err != nil {
			t.Errorf("release: %v", err)
		}
	}()
	m := newTestModel()
	m.draftFiles = run
	first := make(chan tea.Msg, 1)
	go func() { first <- m.armDraftBeatWatch()() }()
	var msg tea.Msg
	select {
	case msg = <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("the failed beat was never reported")
	}
	failure, ok := msg.(draftBeatFailedMsg)
	if !ok {
		t.Fatalf("want draftBeatFailedMsg, got %T", msg)
	}
	cmd, _ := m.handleMsg(failure)
	if !strings.Contains(m.flash, "panne durable") {
		t.Fatalf("the flash must name the failure, got %q", m.flash)
	}
	if cmd == nil {
		t.Fatal("the watch must be re-armed for a later transition")
	}
	again := make(chan tea.Msg, 1)
	go func() { again <- cmd() }()
	select {
	case extra := <-again:
		t.Fatalf("healthy beats must not produce another message, got %#v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}
