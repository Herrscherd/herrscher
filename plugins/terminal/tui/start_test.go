package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func startModel(backend Backend) (*model, tea.Cmd) {
	m := newModel(testMessages(), backend)
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
	files, globErr := filepath.Glob("*.go")
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(files) == 0 || !slices.Contains(files, "start_test.go") {
		t.Fatalf("scanned %d files, want start_test.go among them", len(files))
	}
	fset := token.NewFileSet()
	parsedFiles := make([]*ast.File, 0, len(files))
	for _, file := range files {
		parsed, parseErr := parser.ParseFile(fset, file, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		parsedFiles = append(parsedFiles, parsed)
	}
	info, err := typesInfo(fset, parsedFiles)
	if err != nil {
		t.Log(err)
	}
	var bypasses []string
	for index, parsed := range parsedFiles {
		if !strings.HasSuffix(files[index], "_test.go") {
			continue
		}
		bypasses = append(bypasses, referencesToNewModel(fset, parsed, info, exemptFunction(files[index]))...)
	}
	return bypasses
}

func typesInfo(fset *token.FileSet, files []*ast.File) (*types.Info, error) {
	info := &types.Info{Uses: make(map[*ast.Ident]types.Object)}
	conf := types.Config{Error: func(error) {}}
	_, err := conf.Check("tui", fset, files, info)
	return info, err
}

func exemptFunction(file string) string {
	if file == "start_test.go" {
		return "startModel"
	}
	return ""
}

func referencesToNewModel(fset *token.FileSet, file *ast.File, info *types.Info, exempt string) []string {
	allowed := nilNewModelCallees(file, info)
	var refs []string
	ast.Inspect(file, func(node ast.Node) bool {
		if fn, isFunc := node.(*ast.FuncDecl); isFunc && fn.Recv == nil && fn.Name.Name == exempt {
			return false
		}
		if ident, isIdent := node.(*ast.Ident); isIdent && ident.Name == "newModel" && !allowed[ident] {
			refs = append(refs, fset.Position(ident.Pos()).String())
		}
		return true
	})
	return refs
}

func nilNewModelCallees(file *ast.File, info *types.Info) map[*ast.Ident]bool {
	allowed := make(map[*ast.Ident]bool)
	ast.Inspect(file, func(node ast.Node) bool {
		if callee, isNilCall := newModelNilCallee(node, info); isNilCall {
			allowed[callee] = true
		}
		return true
	})
	return allowed
}

func newModelNilCallee(node ast.Node, info *types.Info) (*ast.Ident, bool) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall || len(call.Args) != 2 {
		return nil, false
	}
	arg, isArgIdent := call.Args[1].(*ast.Ident)
	if !isArgIdent {
		return nil, false
	}
	if _, isPredeclaredNil := info.Uses[arg].(*types.Nil); !isPredeclaredNil {
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
	_ = newModel(testMessages(), nil)
	_ = newModel(testMessages(), be)
}

func shadowed() {
	var nil Backend
	_ = newModel(testMessages(), nil)
}
`
	fset := token.NewFileSet()
	parsed, parseErr := parser.ParseFile(fset, "snippet.go", snippet, 0)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	info, err := typesInfo(fset, []*ast.File{parsed})
	if err != nil {
		t.Log(err)
	}
	refs := referencesToNewModel(fset, parsed, info, "")
	want := []string{"snippet.go:4:7", "snippet.go:5:11", "snippet.go:7:6", "snippet.go:12:6"}
	if strings.Join(refs, " ") != strings.Join(want, " ") {
		t.Fatalf("flagged = %q, want %q", refs, want)
	}
}

func TestNewModelGuardResolvesNilAcrossFiles(t *testing.T) {
	const declaration = `package tui

var nil Backend
`
	const fixture = `package tui

func fixtures(be Backend) {
	_ = newModel(testMessages(), nil)
	_ = newModel(testMessages(), be)
}
`
	fset := token.NewFileSet()
	declared, declaredErr := parser.ParseFile(fset, "nil_decl.go", declaration, 0)
	if declaredErr != nil {
		t.Fatal(declaredErr)
	}
	parsed, parseErr := parser.ParseFile(fset, "fixtures_test.go", fixture, 0)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	info, err := typesInfo(fset, []*ast.File{declared, parsed})
	if err != nil {
		t.Log(err)
	}
	refs := referencesToNewModel(fset, parsed, info, "")
	want := []string{"fixtures_test.go:4:6", "fixtures_test.go:5:6"}
	if strings.Join(refs, " ") != strings.Join(want, " ") {
		t.Fatalf("flagged = %q, want %q: a package-level nil is not the predeclared one", refs, want)
	}
}

func TestNewModelGuardExemptsOnlyTheNamedFunction(t *testing.T) {
	const snippet = `package tui

func startModel(backend Backend) {
	_ = newModel(testMessages(), backend)
}

func other(be Backend) {
	_ = newModel(testMessages(), be)
}

func (m *model) startModel(be Backend) {
	_ = newModel(testMessages(), be)
}
`
	fset := token.NewFileSet()
	parsed, parseErr := parser.ParseFile(fset, "start_test.go", snippet, 0)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	info, err := typesInfo(fset, []*ast.File{parsed})
	if err != nil {
		t.Log(err)
	}
	refs := referencesToNewModel(fset, parsed, info, "startModel")
	want := []string{"start_test.go:8:6", "start_test.go:12:6"}
	if strings.Join(refs, " ") != strings.Join(want, " ") {
		t.Fatalf("flagged = %q, want %q: only the exempt function may reference newModel", refs, want)
	}
}
