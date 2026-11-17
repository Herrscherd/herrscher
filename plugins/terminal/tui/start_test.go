package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func startModel(backend Backend) (*model, tea.Cmd) {
	m := newModel(backend)
	if backend == nil {
		return m, nil
	}
	m.sessionsIssued++
	m.sessionsInFlight = true
	_, cmd := m.Update(sessionsMsg{seq: m.sessionsIssued, infos: backend.Sessions()})
	return m, cmd
}

func startedModel(backend Backend) *model {
	m, cmd := startModel(backend)
	deliver(m, cmd)
	return m
}

func deliver(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, child := range msg {
			deliver(m, child)
		}
	default:
		_, next := m.Update(msg)
		deliver(m, next)
	}
}

func TestFixturesWithABackendStartThroughStartModel(t *testing.T) {
	if bypasses := newModelBypasses(t); len(bypasses) > 0 {
		t.Fatalf("fixtures with a backend must start through startModel, not newModel: %s", strings.Join(bypasses, ", "))
	}
}

func newModelBypasses(t *testing.T) []string {
	t.Helper()
	files, globErr := filepath.Glob("*_test.go")
	if globErr != nil {
		t.Fatal(globErr)
	}
	fset := token.NewFileSet()
	var bypasses []string
	for _, file := range files {
		if file == "start_test.go" {
			continue
		}
		parsed, parseErr := parser.ParseFile(fset, file, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if isBackedNewModel(node) {
				bypasses = append(bypasses, fset.Position(node.Pos()).String())
			}
			return true
		})
	}
	return bypasses
}

func isBackedNewModel(node ast.Node) bool {
	call, isCall := node.(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return false
	}
	fn, isIdent := call.Fun.(*ast.Ident)
	if !isIdent || fn.Name != "newModel" {
		return false
	}
	arg, isArgIdent := call.Args[0].(*ast.Ident)
	return !isArgIdent || arg.Name != "nil"
}
