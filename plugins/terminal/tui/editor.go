package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	fallbackEditor = "vi"
	draftFile      = "draft-*.md"
	draftDirPrefix = "herrscher-drafts-"
	draftDirMode   = 0o700
)

type editorDoneMsg struct {
	path string
	err  error
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

func draftDir(root string) string {
	return filepath.Join(root, draftDirPrefix+strconv.Itoa(os.Getpid()))
}

func writeDraft(text string) (string, error) {
	dir := draftDir(os.TempDir())
	if err := os.MkdirAll(dir, draftDirMode); err != nil {
		return "", err
	}
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

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone)
}

func removeOrphanDrafts(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		pid, ok := draftOwner(entry.Name())
		if !ok || processAlive(pid) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func draftOwner(name string) (int, bool) {
	rest, found := strings.CutPrefix(name, draftDirPrefix)
	if !found {
		return 0, false
	}
	pid, err := strconv.Atoi(rest)
	return pid, err == nil && pid > 0
}

func (m *model) openEditor() tea.Cmd {
	path, err := writeDraft(m.input.Value())
	if err != nil {
		m.flash = "éditeur indisponible : " + err.Error()
		return nil
	}
	m.editing = true
	return tea.ExecProcess(editorCommand(m.runContext(), path), func(err error) tea.Msg {
		return editorDoneMsg{path: path, err: err}
	})
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
