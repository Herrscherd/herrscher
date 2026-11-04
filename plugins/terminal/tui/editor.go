package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	fallbackEditor = "vi"
	editorPattern  = "herrscher-draft-*.md"
	draftMaxAge    = 24 * time.Hour
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

func writeDraft(text string) (string, error) {
	file, err := os.CreateTemp("", editorPattern)
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

func removeStaleDrafts(dir string, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		if !isDraftName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if now.Sub(info.ModTime()) < draftMaxAge {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func isDraftName(name string) bool {
	matched, err := filepath.Match(editorPattern, name)
	return err == nil && matched
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
