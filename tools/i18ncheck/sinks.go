package main

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode"
)

type sinkWalker struct {
	files      *token.FileSet
	translated map[string]bool
	exempt     map[ast.Node]bool
	found      []finding
}

func findPackageSinks(files *token.FileSet, parsed []*ast.File) []finding {
	walker := &sinkWalker{
		files:      files,
		translated: translatedSentinels(parsed),
		exempt:     make(map[ast.Node]bool),
	}
	for _, file := range parsed {
		ast.Inspect(file, walker.visit)
	}
	return walker.found
}

func (walker *sinkWalker) visit(node ast.Node) bool {
	switch typed := node.(type) {
	case *ast.ValueSpec:
		walker.exemptTranslated(typed)
	case *ast.AssignStmt:
		walker.found = append(walker.found, assignmentFindings(walker.files, typed)...)
	case *ast.CallExpr:
		if !walker.exempt[typed] {
			walker.found = append(walker.found, callFindings(walker.files, typed)...)
		}
	}
	return true
}

func (walker *sinkWalker) exemptTranslated(spec *ast.ValueSpec) {
	for index, name := range spec.Names {
		if index < len(spec.Values) && walker.translated[name.Name] {
			walker.exempt[spec.Values[index]] = true
		}
	}
}

func translatedSentinels(parsed []*ast.File) map[string]bool {
	translated := make(map[string]bool)
	for _, file := range parsed {
		ast.Inspect(file, func(node ast.Node) bool {
			if name, ok := keyedSentinel(node); ok {
				translated[name] = true
			}
			return true
		})
	}
	return translated
}

func keyedSentinel(node ast.Node) (string, bool) {
	pair, ok := node.(*ast.CompositeLit)
	if !ok || len(pair.Elts) != 2 {
		return "", false
	}
	name, isIdent := pair.Elts[0].(*ast.Ident)
	key, isText := pair.Elts[1].(*ast.BasicLit)
	if !isIdent || !isText || !isCatalogKey(key) {
		return "", false
	}
	return name.Name, true
}

func isCatalogKey(literal *ast.BasicLit) bool {
	key, ok := unquote(literal)
	if !ok || !strings.Contains(key, ".") {
		return false
	}
	return strings.IndexFunc(key, isNotKeyRune) < 0
}

func isNotKeyRune(letter rune) bool {
	return letter != '.' && letter != '_' && !unicode.IsLower(letter) && !unicode.IsDigit(letter)
}

func assignmentFindings(files *token.FileSet, assign *ast.AssignStmt) []finding {
	if len(assign.Lhs) != len(assign.Rhs) {
		return nil
	}
	var found []finding
	for index, target := range assign.Lhs {
		if !isShownField(target) {
			continue
		}
		if sentence, ok := proseOf(files, assign.Rhs[index]); ok {
			found = append(found, sentence)
		}
	}
	return found
}

func isShownField(target ast.Expr) bool {
	selector, ok := target.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	name := strings.ToLower(selector.Sel.Name)
	return name == "flash" || strings.HasSuffix(name, "notice")
}

func callFindings(files *token.FileSet, call *ast.CallExpr) []finding {
	if len(call.Args) == 0 || !isShowingCall(call.Fun) {
		return nil
	}
	if sentence, ok := proseOf(files, call.Args[0]); ok {
		return []finding{sentence}
	}
	return nil
}

func isShowingCall(fun ast.Expr) bool {
	selector, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch qualifier.Name + "." + selector.Sel.Name {
	case "contracts.DataResult", "fmt.Errorf", "errors.New":
		return true
	}
	return false
}

func proseOf(files *token.FileSet, expr ast.Expr) (finding, bool) {
	text, ok := literalText(expr)
	if !ok || !isProse(text) {
		return finding{}, false
	}
	return finding{position: files.Position(expr.Pos()), text: text}, true
}

func literalText(expr ast.Expr) (string, bool) {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		return unquote(typed)
	case *ast.CallExpr:
		return formatText(typed)
	}
	return "", false
}

func formatText(call *ast.CallExpr) (string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return "", false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok || qualifier.Name != "fmt" {
		return "", false
	}
	switch selector.Sel.Name {
	case "Sprintf", "Sprint", "Errorf":
	default:
		return "", false
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok {
		return "", false
	}
	return unquote(literal)
}

func unquote(literal *ast.BasicLit) (string, bool) {
	if literal.Kind != token.STRING {
		return "", false
	}
	text, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return text, true
}
