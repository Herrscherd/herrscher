package tui

import (
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

func (m *model) openEditor() tea.Cmd {
	file, err := os.CreateTemp("", editorPattern)
	if err != nil {
		m.flash = "éditeur indisponible : " + err.Error()
		return nil
	}
	path := file.Name()
	if _, err := file.WriteString(m.input.Value()); err != nil {
		file.Close()
		os.Remove(path)
		m.flash = "éditeur indisponible : " + err.Error()
		return nil
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		m.flash = "éditeur indisponible : " + err.Error()
		return nil
	}
	m.editing = true
	return tea.ExecProcess(editorCommand(path), func(err error) tea.Msg {
		return editorDoneMsg{path: path, err: err}
	})
}

func (m *model) applyEditorResult(msg editorDoneMsg) {
	m.editing = false
	defer os.Remove(msg.path)
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
