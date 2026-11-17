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
		bypasses = append(bypasses, referencesToNewModel(fset, parsed)...)
	}
	return bypasses
}

func referencesToNewModel(fset *token.FileSet, file *ast.File) []string {
	allowed := nilNewModelCallees(file)
	var refs []string
	ast.Inspect(file, func(node ast.Node) bool {
		if ident, isIdent := node.(*ast.Ident); isIdent && ident.Name == "newModel" && !allowed[ident] {
			refs = append(refs, fset.Position(ident.Pos()).String())
		}
		return true
	})
	return refs
}

func nilNewModelCallees(file *ast.File) map[*ast.Ident]bool {
	allowed := make(map[*ast.Ident]bool)
	ast.Inspect(file, func(node ast.Node) bool {
		if callee, isNilCall := newModelNilCallee(node); isNilCall {
			allowed[callee] = true
		}
		return true
	})
	return allowed
}

func newModelNilCallee(node ast.Node) (*ast.Ident, bool) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return nil, false
	}
	arg, isArgIdent := call.Args[0].(*ast.Ident)
	if !isArgIdent || arg.Name != "nil" {
		return nil, false
	}
	callee, isIdent := call.Fun.(*ast.Ident)
	return callee, isIdent && callee.Name == "newModel"
}

func TestNewModelGuardFlagsEveryReferenceButNilCalls(t *testing.T) {
	const snippet = `package tui

func fixtures(be Backend) {
	_ = (newModel)(be)
	build := newModel
	_ = newModel(nil)
	_ = newModel(be)
}
`
	fset := token.NewFileSet()
	parsed, parseErr := parser.ParseFile(fset, "snippet.go", snippet, 0)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	refs := referencesToNewModel(fset, parsed)
	want := []string{"snippet.go:4:7", "snippet.go:5:11", "snippet.go:7:7"}
	if strings.Join(refs, " ") != strings.Join(want, " ") {
		t.Fatalf("flagged = %q, want %q", refs, want)
	}
}
