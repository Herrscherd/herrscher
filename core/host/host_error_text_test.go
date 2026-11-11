package host

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestEveryHostSentinelIsInTheErrorTable(t *testing.T) {
	declared := sentinelNames(t)
	listed := tableNames(t)
	var missing, extra []string
	for name := range declared {
		if !listed[name] {
			missing = append(missing, name)
		}
	}
	for name := range listed {
		if !declared[name] {
			extra = append(extra, name)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("hostErrorKeys does not match the package sentinels: missing %v, not a sentinel %v", missing, extra)
	}
}

func TestEveryHostErrorHasItsTextInEveryLanguage(t *testing.T) {
	for _, known := range hostErrorKeys() {
		english, err := clitext.Text(i18n.English, clitext.Key(known.key), nil)
		if err != nil {
			t.Fatalf("%s in english: %v", known.key, err)
		}
		if english != known.sentinel.Error() {
			t.Errorf("%s: english %q does not match the sentinel %q", known.key, english, known.sentinel.Error())
		}
		french, err := clitext.Text(i18n.French, clitext.Key(known.key), nil)
		if err != nil {
			t.Fatalf("%s in french: %v", known.key, err)
		}
		if french == "" || french == english {
			t.Errorf("%s: french text %q is not translated", known.key, french)
		}
	}
}

func TestHostErrorIsWrittenInTheCommandLanguage(t *testing.T) {
	err := fmt.Errorf("session %q %w", "alpha", errNoPendingChoice)
	french := localizeHostError(i18n.French, err)
	if want := `session "alpha" n'a aucun choix en attente`; french.Error() != want {
		t.Fatalf("french = %q, want %q", french.Error(), want)
	}
	if !errors.Is(french, errNoPendingChoice) {
		t.Fatal("the localized error lost its sentinel")
	}
	if english := localizeHostError(i18n.English, err); english.Error() != err.Error() {
		t.Fatalf("english = %q, want %q", english.Error(), err.Error())
	}
}

func TestHostErrorsOutsideTheTableAreUnchanged(t *testing.T) {
	other := errors.New("boom")
	if got := localizeHostError(i18n.French, other); !errors.Is(got, other) || got.Error() != other.Error() {
		t.Fatalf("localizeHostError changed an unknown error into %v", got)
	}
}

func hostFiles(t *testing.T) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	return files
}

func sentinelNames(t *testing.T) map[string]bool {
	t.Helper()
	names := make(map[string]bool)
	for _, file := range hostFiles(t) {
		for _, spec := range varSpecs(file) {
			for index, ident := range spec.Names {
				if index < len(spec.Values) && callsErrorsNew(spec.Values[index]) {
					names[ident.Name] = true
				}
			}
		}
	}
	return names
}

func varSpecs(file *ast.File) []*ast.ValueSpec {
	var specs []*ast.ValueSpec
	for _, decl := range file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, spec := range general.Specs {
			if value, ok := spec.(*ast.ValueSpec); ok {
				specs = append(specs, value)
			}
		}
	}
	return specs
}

func callsErrorsNew(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if isErrorsNew(node) {
			found = true
		}
		return !found
	})
	return found
}

func isErrorsNew(node ast.Node) bool {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "New" {
		return false
	}
	owner, ok := selector.X.(*ast.Ident)
	return ok && owner.Name == "errors"
}

func tableNames(t *testing.T) map[string]bool {
	t.Helper()
	names := make(map[string]bool)
	for _, file := range hostFiles(t) {
		for _, body := range hostErrorKeysBodies(file) {
			ast.Inspect(body, func(node ast.Node) bool {
				recordTableEntry(node, names)
				return true
			})
		}
	}
	return names
}

func hostErrorKeysBodies(file *ast.File) []*ast.BlockStmt {
	var bodies []*ast.BlockStmt
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if ok && function.Name.Name == "hostErrorKeys" && function.Body != nil {
			bodies = append(bodies, function.Body)
		}
	}
	return bodies
}

func recordTableEntry(node ast.Node, names map[string]bool) {
	pair, ok := node.(*ast.CompositeLit)
	if !ok || len(pair.Elts) != 2 {
		return
	}
	if ident, ok := pair.Elts[0].(*ast.Ident); ok {
		names[ident.Name] = true
	}
}

func TestRegistryCommandErrorsAreWrittenInTheHostLanguage(t *testing.T) {
	reg, err := NewRegistry(context.Background(), nil, Options{Runtime: testRuntime(t), StatePath: t.TempDir() + "/s.json", Language: i18n.French})
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Dispatch(context.Background(), []string{"role", "revoke"})
	if want := "nommez un principal (voir `role list`)"; err == nil || err.Error() != want {
		t.Fatalf("role revoke error = %v, want %q", err, want)
	}
	if !errors.Is(err, errPrincipalMissing) {
		t.Fatal("the localized registry error lost its sentinel")
	}
}

func TestRegistryErrorsFollowTheLiveOperatorLanguage(t *testing.T) {
	live := newOperatorLanguage(i18n.English)
	reg, err := NewRegistry(context.Background(), nil, Options{Runtime: testRuntime(t), StatePath: t.TempDir() + "/s.json", Language: i18n.English, live: live})
	if err != nil {
		t.Fatal(err)
	}
	live.set(i18n.French)
	_, err = reg.Dispatch(context.Background(), []string{"role", "revoke"})
	if want := "nommez un principal (voir `role list`)"; err == nil || err.Error() != want {
		t.Fatalf("role revoke error = %v, want %q after the live switch", err, want)
	}
}
