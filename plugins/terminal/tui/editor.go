package tui

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	fallbackEditor = "vi"
	editorPattern  = "herrscher-draft-*.md"
)

type editorDoneMsg struct {
	path string
	err  error
}

func editorArgv(path string) []string {
	editor := os.Getenv("EDITOR")
	if strings.TrimSpace(editor) == "" {
		editor = os.Getenv("VISUAL")
	}
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		fields = []string{fallbackEditor}
	}
	return append(fields, path)
}

func editorCommand(path string) *exec.Cmd {
	argv := editorArgv(path)
	return exec.Command(argv[0], argv[1:]...)
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

func (m *model) openEditor() tea.Cmd {
	path, err := writeDraft(m.input.Value())
	if err != nil {
		m.flash = "éditeur indisponible : " + err.Error()
		return nil
	}
	m.editing = true
	return tea.ExecProcess(editorCommand(path), func(err error) tea.Msg {
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
