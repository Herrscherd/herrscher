package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func declaredMessageKeys(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "keys.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			if key, ok := msgKeyOf(t, spec); ok {
				keys = append(keys, key)
			}
		}
	}
	if len(keys) == 0 {
		t.Fatal("no msgKey constant found in keys.go")
	}
	return keys
}

func msgKeyOf(t *testing.T, spec ast.Spec) (string, bool) {
	t.Helper()
	value, ok := spec.(*ast.ValueSpec)
	if !ok || !isMsgKeyType(value.Type) || len(value.Values) != 1 {
		return "", false
	}
	lit, ok := value.Values[0].(*ast.BasicLit)
	if !ok {
		t.Fatalf("msgKey constant %v is not a string literal", value.Names)
	}
	key, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatal(err)
	}
	return key, true
}

func isMsgKeyType(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "msgKey"
}

func catalogueIDs(t *testing.T, lang string) []string {
	t.Helper()
	raw, err := os.ReadFile("locales/" + lang + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

func TestEveryDeclaredKeyIsInBothCatalogues(t *testing.T) {
	declared := declaredMessageKeys(t)
	for _, lang := range []string{"en", "fr"} {
		ids := catalogueIDs(t, lang)
		for _, key := range declared {
			if !slices.Contains(ids, key) {
				t.Errorf("key %q is declared in keys.go but missing from %s.json", key, lang)
			}
		}
	}
}

func TestUsageWritesInEachLanguage(t *testing.T) {
	msgs := loadCatalogue()
	descriptions := make(map[i18n.Language]string)
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		var out bytes.Buffer
		if err := writeUsage(&out, msgs, lang); err != nil {
			t.Fatalf("writeUsage(%v): %v", lang, err)
		}
		descriptions[lang] = flagDescription(t, out.String(), "api-key-env")
	}
	if descriptions[i18n.French] == descriptions[i18n.English] {
		t.Fatalf("the api-key-env description is the English one in French: the French catalogue lacks it")
	}
}

func flagDescription(t *testing.T, usage, name string) string {
	t.Helper()
	header := "  -" + name + "\n    "
	_, rest, found := strings.Cut(usage, header)
	if !found {
		t.Fatalf("usage lacks the -%s flag:\n%s", name, usage)
	}
	description, _, _ := strings.Cut(rest, "\n")
	return description
}
