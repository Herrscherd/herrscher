package productionlayout

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const (
	moduleRoot         = "../.."
	entryPointFile     = "main.go"
	unixWrappersFile   = "core/host/cmdsocket_unix.go"
	runtimeDirFile     = "core/host/runtime_dir.go"
	runtimedirPackage  = "core/internal/runtimedir/"
	productionRuntime  = "ProductionRuntime"
	productionLayout   = "Production"
	commandSocketCall  = "CommandSocketPath"
	eventsSocketCall   = "EventsSocketPath"
	contractTestFile   = "core/host/cmdsocket_path_unix_test.go"
	contractTestFunc   = "TestPublicSocketPathsShareOneDirectory"
	testFileSuffix     = "_test.go"
	nodeModulesDirName = "node_modules"
)

type sourceFile struct {
	rel  string
	fset *token.FileSet
	tree *ast.File
}

func TestProductionRuntimeIsReachedOnlyFromTheEntryPointAndTheUnixWrappers(t *testing.T) {
	for _, file := range parseModule(t) {
		if productionRuntimeAllowed(file.rel) {
			continue
		}
		ast.Inspect(file.tree, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == productionRuntime {
				t.Errorf("%s: references ProductionRuntime; only %s, %s and %s may", file.position(ident.Pos()), entryPointFile, unixWrappersFile, runtimeDirFile)
			}
			return true
		})
	}
}

func TestTheProductionLayoutConstructorIsUsedOnlyByTheRuntimeDirectory(t *testing.T) {
	for _, file := range parseModule(t) {
		if file.rel == runtimeDirFile || strings.HasPrefix(file.rel, runtimedirPackage) {
			continue
		}
		ast.Inspect(file.tree, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == productionLayout {
				t.Errorf("%s: calls the production layout constructor; only %s and the runtimedir package may", file.position(ident.Pos()), runtimeDirFile)
			}
			return true
		})
	}
}

func TestTestsNeverCallTheProductionSocketWrappers(t *testing.T) {
	for _, file := range parseModule(t) {
		if !strings.HasSuffix(file.rel, testFileSuffix) {
			continue
		}
		for _, decl := range file.tree.Decls {
			if isContractTest(file.rel, decl) {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if ident, ok := n.(*ast.Ident); ok && (ident.Name == commandSocketCall || ident.Name == eventsSocketCall) {
					t.Errorf("%s: uses %s; a test must pass an explicit Layout, only %s may use the production wrappers", file.position(ident.Pos()), ident.Name, contractTestFunc)
				}
				return true
			})
		}
	}
}

func productionRuntimeAllowed(rel string) bool {
	switch rel {
	case entryPointFile, unixWrappersFile, runtimeDirFile:
		return true
	}
	return false
}

func isContractTest(rel string, decl ast.Decl) bool {
	fn, ok := decl.(*ast.FuncDecl)
	return ok && rel == contractTestFile && fn.Name.Name == contractTestFunc
}

func (f sourceFile) position(pos token.Pos) token.Position {
	return f.fset.Position(pos)
}

func parseModule(t *testing.T) []sourceFile {
	t.Helper()
	var files []sourceFile
	err := filepath.WalkDir(moduleRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skippedDir(p, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" {
			return nil
		}
		file, err := parseSource(p)
		if err != nil {
			return err
		}
		files = append(files, file)
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}
	if !hasEntryPoint(files) {
		t.Fatalf("the module walk found no %s, the rules would pass on nothing", entryPointFile)
	}
	return files
}

func skippedDir(p, name string) bool {
	return p != moduleRoot && (strings.HasPrefix(name, ".") || name == nodeModulesDirName)
}

func parseSource(p string) (sourceFile, error) {
	rel, err := filepath.Rel(moduleRoot, p)
	if err != nil {
		return sourceFile{}, err
	}
	fset := token.NewFileSet()
	tree, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
	if err != nil {
		return sourceFile{}, err
	}
	return sourceFile{rel: filepath.ToSlash(rel), fset: fset, tree: tree}, nil
}

func hasEntryPoint(files []sourceFile) bool {
	for _, file := range files {
		if file.rel == entryPointFile {
			return true
		}
	}
	return false
}
