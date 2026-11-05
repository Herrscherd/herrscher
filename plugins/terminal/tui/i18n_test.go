package tui

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n/i18ntest"
)

func TestCatalogGuards(t *testing.T) {
	sources, err := fs.Sub(localeFiles, "locales")
	if err != nil {
		t.Fatal(err)
	}
	i18ntest.Check(t, sources)
}

func TestCatalogMatchesTranslatedCalls(t *testing.T) {
	declared, err := declaredMessages(".")
	if err != nil {
		t.Fatal(err)
	}
	sources, err := fs.Sub(localeFiles, "locales")
	if err != nil {
		t.Fatal(err)
	}
	i18ntest.CheckDeclared(t, declared, sources)
}

func declaredMessages(dir string) (map[string][]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	parsedFiles, err := parseSources(fset, files)
	if err != nil {
		return nil, err
	}
	constants := make(map[string]string)
	for _, parsed := range parsedFiles {
		collectStringConstants(parsed, constants)
	}
	declared := make(map[string][]string)
	mentioned := make(map[string]bool)
	for _, parsed := range parsedFiles {
		if callErr := collectTrCalls(fset, parsed, constants, declared); callErr != nil {
			return nil, callErr
		}
		collectMentionedKeys(parsed, mentioned)
	}
	english, err := englishFields()
	if err != nil {
		return nil, err
	}
	for key := range mentioned {
		if _, known := declared[key]; !known {
			declared[key] = english[key]
		}
	}
	return declared, nil
}

func collectMentionedKeys(file *ast.File, mentioned map[string]bool) {
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok {
			return true
		}
		if value, ok := stringLiteral(literal); ok && strings.HasPrefix(value, "tui.") {
			mentioned[value] = true
		}
		return true
	})
}

var fieldPlaceholder = regexp.MustCompile(`\{\{\.(\w+)\}\}`)

func englishFields() (map[string][]string, error) {
	raw, err := localeFiles.ReadFile("locales/en.json")
	if err != nil {
		return nil, err
	}
	var entries []struct {
		ID    string `json:"id"`
		One   string `json:"one"`
		Other string `json:"other"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	fields := make(map[string][]string)
	for _, entry := range entries {
		var names []string
		for _, text := range []string{entry.One, entry.Other} {
			for _, match := range fieldPlaceholder.FindAllStringSubmatch(text, -1) {
				names = append(names, match[1])
			}
		}
		slices.Sort(names)
		fields[entry.ID] = slices.Compact(names)
	}
	return fields, nil
}

func parseSources(fset *token.FileSet, files []string) ([]*ast.File, error) {
	parsedFiles := []*ast.File{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		parsed, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			return nil, err
		}
		parsedFiles = append(parsedFiles, parsed)
	}
	return parsedFiles, nil
}

func collectStringConstants(file *ast.File, constants map[string]string) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			if valueSpec, ok := spec.(*ast.ValueSpec); ok {
				addStringConstants(valueSpec, constants)
			}
		}
	}
}

func addStringConstants(spec *ast.ValueSpec, constants map[string]string) {
	for index, name := range spec.Names {
		if index >= len(spec.Values) {
			return
		}
		if value, ok := stringLiteral(spec.Values[index]); ok {
			constants[name.Name] = value
		}
	}
}

func collectTrCalls(fset *token.FileSet, file *ast.File, constants map[string]string, declared map[string][]string) error {
	var failure error
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || failure != nil {
			return failure == nil
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "tr" || len(call.Args) != 3 {
			return true
		}
		key, ok := stringLiteral(call.Args[1])
		if !ok {
			return true
		}
		fields, err := mapFields(fset, call, constants)
		if err != nil {
			failure = err
			return false
		}
		if previous, seen := declared[key]; seen && !slices.Equal(previous, fields) {
			failure = &trError{fset.Position(call.Pos()).String(), "key " + key + " is called with different fields"}
			return false
		}
		declared[key] = fields
		return true
	})
	return failure
}

func mapFields(fset *token.FileSet, call *ast.CallExpr, constants map[string]string) ([]string, error) {
	literal, ok := call.Args[2].(*ast.CompositeLit)
	if !ok {
		if isNil(call.Args[2]) {
			return []string{}, nil
		}
		return nil, &trError{fset.Position(call.Pos()).String(), "parameters must be a map literal or nil"}
	}
	fields := []string{}
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return nil, &trError{fset.Position(element.Pos()).String(), "parameter must be a key value pair"}
		}
		name, ok := fieldName(pair.Key, constants)
		if !ok {
			return nil, &trError{fset.Position(element.Pos()).String(), "parameter name is not a string literal or a string constant"}
		}
		fields = append(fields, name)
	}
	slices.Sort(fields)
	return fields, nil
}

func fieldName(expr ast.Expr, constants map[string]string) (string, bool) {
	if ident, ok := expr.(*ast.Ident); ok {
		value, found := constants[ident.Name]
		return value, found
	}
	return stringLiteral(expr)
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

func isNil(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "nil"
}

type trError struct {
	position string
	reason   string
}

func (e *trError) Error() string {
	return e.position + ": " + e.reason
}
