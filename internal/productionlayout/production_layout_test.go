package productionlayout

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	moduleRoot          = "../.."
	entryPointFile      = "main.go"
	unixWrappersFile    = "core/host/cmdsocket_unix.go"
	windowsWrappersFile = "core/host/cmdsocket_windows.go"
	runtimeDirFile      = "core/host/runtime_dir.go"
	runtimedirPackage   = "core/internal/runtimedir/"
	runtimedirImport    = "github.com/Herrscherd/herrscher/core/internal/runtimedir"
	runtimedirName      = "runtimedir"
	hostImport          = "github.com/Herrscherd/herrscher/core/host"
	hostName            = "host"
	productionRuntime   = "ProductionRuntime"
	productionLayout    = "Production"
	runtimedirAt        = "At"
	hostRuntimeAt       = "RuntimeAt"
	commandSocketCall   = "CommandSocketPath"
	eventsSocketCall    = "EventsSocketPath"
	contractTestFile    = "core/host/cmdsocket_path_unix_test.go"
	contractTestFunc    = "TestPublicSocketPathsShareOneDirectory"
	testFileSuffix      = "_test.go"
	nodeModulesDirName  = "node_modules"
	gitDirName          = ".git"
)

type sourceFile struct {
	rel  string
	fset *token.FileSet
	tree *ast.File
}

func TestProductionRuntimeIsReachedOnlyFromTheEntryPointAndTheUnixWrappers(t *testing.T) {
	reportAll(t, productionRuntimeViolations(parseModule(t)))
}

func TestTheProductionLayoutConstructorIsUsedOnlyByTheRuntimeDirectory(t *testing.T) {
	reportAll(t, productionLayoutViolations(parseModule(t)))
}

func TestTestsNeverCallTheProductionSocketWrappers(t *testing.T) {
	reportAll(t, testWrapperViolations(parseModule(t)))
}

func TestProductionCodeNeverCallsTheProductionSocketWrappers(t *testing.T) {
	reportAll(t, productionWrapperViolations(parseModule(t)))
}

func TestTheRuntimeDirectoryExportsNoRootsOrResolver(t *testing.T) {
	reportAll(t, runtimedirSurfaceViolations(parseModule(t)))
}

func TestProductionCodeBuildsNoLayoutFromAnArbitraryPath(t *testing.T) {
	reportAll(t, arbitraryLayoutViolations(parseModule(t)))
}

func reportAll(t *testing.T, violations []string) {
	t.Helper()
	for _, violation := range violations {
		t.Error(violation)
	}
}

func productionRuntimeViolations(files []sourceFile) []string {
	var out []string
	for _, file := range files {
		if productionRuntimeAllowed(file.rel) {
			continue
		}
		out = append(out, namedIdents(file, file.tree, isName(productionRuntime),
			fmt.Sprintf("references ProductionRuntime; only %s, %s and %s may", entryPointFile, unixWrappersFile, runtimeDirFile))...)
	}
	return out
}

func arbitraryLayoutViolations(files []sourceFile) []string {
	var out []string
	for _, file := range files {
		if strings.HasSuffix(file.rel, testFileSuffix) || file.rel == runtimeDirFile || strings.HasPrefix(file.rel, runtimedirPackage) {
			continue
		}
		imports := layoutImportsOf(file.tree)
		ast.Inspect(file.tree, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && imports.buildsArbitraryLayout(call.Fun) {
				out = append(out, file.position(call.Pos())+fmt.Sprintf(": builds a layout from an arbitrary path; only tests and %s may, production takes the layout that main passes", runtimeDirFile))
			}
			return true
		})
	}
	return out
}

type layoutImports struct {
	runtimedir    map[string]bool
	host          map[string]bool
	dotRuntimedir bool
}

func layoutImportsOf(tree *ast.File) layoutImports {
	imports := layoutImports{runtimedir: make(map[string]bool), host: make(map[string]bool)}
	for _, spec := range tree.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		switch path {
		case runtimedirImport:
			if spec.Name != nil && spec.Name.Name == "." {
				imports.dotRuntimedir = true
				continue
			}
			imports.runtimedir[importQualifier(spec, runtimedirName)] = true
		case hostImport:
			imports.host[importQualifier(spec, hostName)] = true
		}
	}
	return imports
}

func importQualifier(spec *ast.ImportSpec, defaultName string) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	return defaultName
}

func (imports layoutImports) buildsArbitraryLayout(fun ast.Expr) bool {
	switch callee := fun.(type) {
	case *ast.Ident:
		return callee.Name == hostRuntimeAt || (imports.dotRuntimedir && callee.Name == runtimedirAt)
	case *ast.SelectorExpr:
		qualifier, ok := callee.X.(*ast.Ident)
		if !ok {
			return false
		}
		switch callee.Sel.Name {
		case runtimedirAt:
			return imports.runtimedir[qualifier.Name]
		case hostRuntimeAt:
			return imports.host[qualifier.Name]
		}
	}
	return false
}

func productionLayoutViolations(files []sourceFile) []string {
	var out []string
	for _, file := range files {
		if file.rel == runtimeDirFile || strings.HasPrefix(file.rel, runtimedirPackage) {
			continue
		}
		out = append(out, namedIdents(file, file.tree, isName(productionLayout),
			fmt.Sprintf("calls the production layout constructor; only %s and the runtimedir package may", runtimeDirFile))...)
	}
	return out
}

func testWrapperViolations(files []sourceFile) []string {
	var out []string
	for _, file := range files {
		if !strings.HasSuffix(file.rel, testFileSuffix) {
			continue
		}
		for _, decl := range file.tree.Decls {
			if isContractTest(file.rel, decl) {
				continue
			}
			out = append(out, namedIdents(file, decl, isSocketWrapper,
				fmt.Sprintf("a test must pass an explicit Layout; only %s may use the production wrappers", contractTestFunc))...)
		}
	}
	return out
}

func productionWrapperViolations(files []sourceFile) []string {
	var out []string
	for _, file := range files {
		if strings.HasSuffix(file.rel, testFileSuffix) || productionWrapperAllowed(file.rel) {
			continue
		}
		out = append(out, namedIdents(file, file.tree, isSocketWrapper,
			fmt.Sprintf("calls a production socket wrapper; only %s, %s and %s may name it, production code takes an explicit Layout", entryPointFile, unixWrappersFile, windowsWrappersFile))...)
	}
	return out
}

func runtimedirSurfaceViolations(files []sourceFile) []string {
	var out []string
	for _, file := range files {
		if strings.HasPrefix(file.rel, runtimedirPackage) {
			continue
		}
		for _, spec := range file.tree.Imports {
			if path, err := strconv.Unquote(spec.Path.Value); err != nil || path != runtimedirImport {
				continue
			}
			out = append(out, importedSurfaceViolations(file, spec)...)
		}
	}
	return out
}

func importedSurfaceViolations(file sourceFile, spec *ast.ImportSpec) []string {
	if spec.Name != nil && spec.Name.Name == "." {
		return namedIdents(file, file.tree, isForbiddenRuntimedirName,
			"uses a runtimedir name that must stay unexported; the directory exports no roots and no resolver")
	}
	qualifier := runtimedirName
	if spec.Name != nil {
		qualifier = spec.Name.Name
	}
	var out []string
	ast.Inspect(file.tree, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !isForbiddenRuntimedirName(sel.Sel.Name) {
			return true
		}
		if qual, ok := sel.X.(*ast.Ident); ok && qual.Name == qualifier {
			out = append(out, file.position(sel.Pos())+": uses runtimedir."+sel.Sel.Name+"; the directory exports no roots and no resolver")
		}
		return true
	})
	return out
}

func isForbiddenRuntimedirName(name string) bool {
	switch name {
	case "Roots", "ProductionRoots", "Resolve", "DirOf":
		return true
	}
	return false
}

func isName(want string) func(string) bool {
	return func(name string) bool { return name == want }
}

func isSocketWrapper(name string) bool {
	return name == commandSocketCall || name == eventsSocketCall
}

func namedIdents(file sourceFile, root ast.Node, match func(string) bool, reason string) []string {
	var out []string
	ast.Inspect(root, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok && match(ident.Name) {
			out = append(out, file.position(ident.Pos())+": "+reason)
		}
		return true
	})
	return out
}

func productionRuntimeAllowed(rel string) bool {
	switch rel {
	case entryPointFile, unixWrappersFile, runtimeDirFile:
		return true
	}
	return false
}

func productionWrapperAllowed(rel string) bool {
	switch rel {
	case entryPointFile, unixWrappersFile, windowsWrappersFile:
		return true
	}
	return false
}

func isContractTest(rel string, decl ast.Decl) bool {
	fn, ok := decl.(*ast.FuncDecl)
	return ok && rel == contractTestFile && fn.Name.Name == contractTestFunc
}

func (f sourceFile) position(pos token.Pos) string {
	return f.fset.Position(pos).String()
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
	return p != moduleRoot && (name == gitDirName || name == nodeModulesDirName)
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
