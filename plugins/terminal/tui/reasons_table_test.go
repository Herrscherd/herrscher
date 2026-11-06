package tui

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type tableEntry struct {
	sentinel string
	key      string
}

func packageFiles(t *testing.T) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	return files
}

func declaredSentinels(files []*ast.File) []string {
	var names []string
	for _, file := range files {
		for _, decl := range file.Decls {
			names = append(names, sentinelNamesOf(decl)...)
		}
	}
	return names
}

func sentinelNamesOf(decl ast.Decl) []string {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.VAR {
		return nil
	}
	var names []string
	for _, spec := range gen.Specs {
		names = append(names, sentinelNamesIn(spec)...)
	}
	return names
}

func sentinelNamesIn(spec ast.Spec) []string {
	value, ok := spec.(*ast.ValueSpec)
	if !ok {
		return nil
	}
	var names []string
	for i, name := range value.Names {
		if i < len(value.Values) && isErrorsNew(value.Values[i]) {
			names = append(names, name.Name)
		}
	}
	return names
}

func isErrorsNew(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "errors" && sel.Sel.Name == "New"
}

func tableEntries(t *testing.T, files []*ast.File) []tableEntry {
	t.Helper()
	body := reasonKeysBody(files)
	if body == nil {
		return nil
	}
	var entries []tableEntry
	ast.Inspect(body, func(node ast.Node) bool {
		if lit, ok := node.(*ast.CompositeLit); ok {
			if entry, ok := tableEntryOf(t, lit); ok {
				entries = append(entries, entry)
			}
		}
		return true
	})
	return entries
}

func reasonKeysBody(files []*ast.File) *ast.BlockStmt {
	for _, file := range files {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "reasonKeys" {
				return fn.Body
			}
		}
	}
	return nil
}

func tableEntryOf(t *testing.T, lit *ast.CompositeLit) (tableEntry, bool) {
	t.Helper()
	if len(lit.Elts) != 2 {
		return tableEntry{}, false
	}
	sentinel, okSentinel := lit.Elts[0].(*ast.Ident)
	raw, okKey := lit.Elts[1].(*ast.BasicLit)
	if !okSentinel || !okKey {
		return tableEntry{}, false
	}
	key, err := strconv.Unquote(raw.Value)
	if err != nil {
		t.Fatal(err)
	}
	return tableEntry{sentinel: sentinel.Name, key: key}, true
}

func neverShownSentinels() []string {
	return []string{
		"errEmptyImage",
		"errImageTooLarge",
		"errNotFetchable",
		"errNoHost",
		"errNotAllowlisted",
		"errStatuslineNoCommand",
		"errStatuslineEmpty",
	}
}

func TestEveryShownSentinelHasAReasonKey(t *testing.T) {
	files := packageFiles(t)
	entries := tableEntries(t, files)
	if len(entries) == 0 {
		t.Fatal("reasonKeys holds no entry")
	}
	var tabled []string
	for _, entry := range entries {
		tabled = append(tabled, entry.sentinel)
	}
	declared := declaredSentinels(files)
	silent := neverShownSentinels()
	for _, name := range declared {
		if !slices.Contains(tabled, name) && !slices.Contains(silent, name) {
			t.Errorf("sentinel %s is neither in reasonKeys nor in neverShownSentinels: a shown error would stay raw", name)
		}
	}
	for _, name := range silent {
		if !slices.Contains(declared, name) || slices.Contains(tabled, name) {
			t.Errorf("neverShownSentinels lists %s, which is not a package sentinel outside reasonKeys", name)
		}
	}
}

func TestReasonKeysExistInBothCatalogues(t *testing.T) {
	entries := tableEntries(t, packageFiles(t))
	for _, lang := range []string{"en", "fr"} {
		raw, err := localeFiles.ReadFile("locales/" + lang + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var messages []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &messages); err != nil {
			t.Fatal(err)
		}
		ids := make(map[string]bool, len(messages))
		for _, message := range messages {
			ids[message.ID] = true
		}
		for _, entry := range entries {
			if !ids[entry.key] {
				t.Errorf("reason key %s (%s) has no entry in %s.json", entry.key, entry.sentinel, lang)
			}
		}
	}
}
