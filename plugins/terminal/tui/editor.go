package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	fallbackEditor = "vi"
	draftFile      = "draft-*.md"
	draftRunPrefix = "run-"
	draftLockName  = ".lock"
	draftDirMode   = 0o700
	draftRetention = 7 * 24 * time.Hour
)

var errUnsafeDraftDir = errors.New("dossier des brouillons ouvert à d'autres ou aux droits trop larges")

type editorDoneMsg struct {
	path string
	err  error
}

type draftRun struct {
	dir  string
	lock *os.File
}

func configuredEditor(env func(string) string) []string {
	if fields := strings.Fields(env("EDITOR")); len(fields) > 0 {
		return fields
	}
	return strings.Fields(env("VISUAL"))
}

func editorArgv(path string) []string {
	fields := configuredEditor(os.Getenv)
	if len(fields) == 0 {
		fields = []string{fallbackEditor}
	}
	return append(fields, path)
}

func editorCommand(ctx context.Context, path string) *exec.Cmd {
	argv := editorArgv(path)
	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

func draftsRoot() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "herrscher", "drafts"), nil
}

func openDraftRun(root string) (*draftRun, error) {
	if err := os.MkdirAll(root, draftDirMode); err != nil {
		return nil, err
	}
	if err := checkPrivateDir(root); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(root, draftRunPrefix)
	if err != nil {
		return nil, err
	}
	if err = checkPrivateDir(dir); err != nil {
		return nil, errors.Join(err, os.RemoveAll(dir))
	}
	lock, err := lockRunDir(dir)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(dir))
	}
	return &draftRun{dir: dir, lock: lock}, nil
}

func checkPrivateDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !privateDir(info) {
		return fmt.Errorf("%w: %s", errUnsafeDraftDir, dir)
	}
	return nil
}

func writeDraftFile(dir, text string) (string, error) {
	file, err := os.CreateTemp(dir, draftFile)
	if err != nil {
		return "", err
	}
	_, writeErr := file.WriteString(text)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", errors.Join(err, os.Remove(file.Name()))
	}
	return file.Name(), nil
}

func sweepDraftsAtStart() error {
	root, err := draftsRoot()
	if err != nil {
		return err
	}
	return sweepDrafts(root)
}

func sweepDrafts(root string) error {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), draftRunPrefix) {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if !sweepable(dir) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func sweepable(dir string) bool {
	info, err := os.Lstat(dir)
	if err != nil || !privateDir(info) {
		return false
	}
	if time.Since(info.ModTime()) < draftRetention {
		return false
	}
	return !runDirHeld(dir)
}

func (m *model) openEditor() tea.Cmd {
	path, err := m.writeDraft(m.input.Value())
	if err != nil {
		m.flash = "éditeur indisponible : " + err.Error()
		return nil
	}
	m.editing = true
	return tea.ExecProcess(editorCommand(m.runContext(), path), func(err error) tea.Msg {
		return editorDoneMsg{path: path, err: err}
	})
}

func (m *model) writeDraft(text string) (string, error) {
	if m.draftFiles == nil {
		root, err := draftsRoot()
		if err != nil {
			return "", err
		}
		run, err := openDraftRun(root)
		if err != nil {
			return "", err
		}
		m.draftFiles = run
	}
	return writeDraftFile(m.draftFiles.dir, text)
}

func (m *model) removeDraft(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.flash = "brouillon non supprimé : " + err.Error()
	}
}

func (m *model) applyEditorResult(msg editorDoneMsg) {
	m.editing = false
	defer m.removeDraft(msg.path)
	if msg.err != nil {
		m.flash = "éditeur fermé en erreur : " + msg.err.Error()
		return
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		m.flash = "relecture impossible : " + err.Error()
		return
	}
	m.input.SetValue(strings.TrimRight(string(data), "\n"))
	m.input.CursorEnd()
	m.resizeComposer()
	m.applySize()
}
