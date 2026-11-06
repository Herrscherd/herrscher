package main

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const dynamicFormatText = "format passed as a parameter"

type sinkWalker struct {
	files      *token.FileSet
	translated map[string]bool
	constants  map[string]ast.Expr
	resolving  map[string]bool
	exempt     map[ast.Node]bool
	found      []finding
}

func findPackageSinks(files *token.FileSet, parsed []*ast.File) []finding {
	walker := &sinkWalker{
		files:      files,
		translated: translatedSentinels(parsed),
		constants:  packageConstants(parsed),
		resolving:  make(map[string]bool),
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
		walker.found = append(walker.found, walker.assignmentFindings(typed)...)
	case *ast.CompositeLit:
		walker.found = append(walker.found, walker.compositeFindings(typed)...)
	case *ast.CallExpr:
		if !walker.exempt[typed] {
			walker.found = append(walker.found, walker.callFindings(typed)...)
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

func packageConstants(parsed []*ast.File) map[string]ast.Expr {
	constants := make(map[string]ast.Expr)
	for _, file := range parsed {
		for _, declaration := range file.Decls {
			addDeclaredValues(constants, declaration)
		}
	}
	return constants
}

func addDeclaredValues(constants map[string]ast.Expr, declaration ast.Decl) {
	general, ok := declaration.(*ast.GenDecl)
	if !ok || (general.Tok != token.CONST && general.Tok != token.VAR) {
		return
	}
	for _, spec := range general.Specs {
		if valueSpec, ok := spec.(*ast.ValueSpec); ok {
			addSpecValues(constants, valueSpec)
		}
	}
}

func addSpecValues(constants map[string]ast.Expr, spec *ast.ValueSpec) {
	for index, name := range spec.Names {
		if index < len(spec.Values) {
			constants[name.Name] = spec.Values[index]
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
	if !isIdent || !isCatalogReference(pair.Elts[1]) {
		return "", false
	}
	return name.Name, true
}

func isCatalogReference(expr ast.Expr) bool {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		return isCatalogKey(typed)
	case *ast.SelectorExpr:
		qualifier, ok := typed.X.(*ast.Ident)
		return ok && qualifier.Name == "clitext"
	}
	return false
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

func (walker *sinkWalker) assignmentFindings(assign *ast.AssignStmt) []finding {
	if len(assign.Lhs) != len(assign.Rhs) {
		return nil
	}
	var found []finding
	for index, target := range assign.Lhs {
		selector, ok := target.(*ast.SelectorExpr)
		if !ok || !isShownName(selector.Sel.Name) {
			continue
		}
		if sentence, ok := walker.proseOf(assign.Rhs[index]); ok {
			found = append(found, sentence)
		}
	}
	return found
}

func (walker *sinkWalker) compositeFindings(literal *ast.CompositeLit) []finding {
	var found []finding
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok || !isShownKey(literal.Type, pair.Key) {
			continue
		}
		if sentence, ok := walker.proseOf(pair.Value); ok {
			found = append(found, sentence)
		}
	}
	return found
}

func isShownKey(literalType, key ast.Expr) bool {
	name, ok := key.(*ast.Ident)
	if !ok {
		return false
	}
	if isQualified(literalType, "contracts", "Event") {
		return name.Name == "Text"
	}
	if isNamedType(literalType, "model") {
		return isShownName(name.Name)
	}
	if isArgType(literalType) {
		return name.Name == "Value"
	}
	return false
}

func isArgType(expr ast.Expr) bool {
	return isQualified(expr, "contracts", "Arg") || isNamedType(expr, "Arg")
}

func isQualified(expr ast.Expr, qualifier, name string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	owner, ok := selector.X.(*ast.Ident)
	return ok && owner.Name == qualifier && selector.Sel.Name == name
}

func isNamedType(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func isShownName(name string) bool {
	lowered := strings.ToLower(name)
	return strings.HasSuffix(lowered, "flash") || strings.HasSuffix(lowered, "notice") || strings.HasSuffix(lowered, "status")
}

func (walker *sinkWalker) callFindings(call *ast.CallExpr) []finding {
	if dynamic, ok := walker.dynamicFormat(call); ok {
		return []finding{dynamic}
	}
	index, ok := showingArgument(call.Fun)
	if !ok || len(call.Args) <= index {
		return nil
	}
	if sentence, ok := walker.proseOf(call.Args[index]); ok {
		return []finding{sentence}
	}
	return nil
}

func (walker *sinkWalker) dynamicFormat(call *ast.CallExpr) (finding, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 || !isFormatFunction(selector) {
		return finding{}, false
	}
	if _, isText := walker.textOf(call.Args[0]); isText {
		return finding{}, false
	}
	return finding{position: walker.files.Position(call.Pos()), text: dynamicFormatText}, true
}

func isFormatFunction(selector *ast.SelectorExpr) bool {
	qualifier, ok := selector.X.(*ast.Ident)
	return ok && qualifier.Name == "fmt" && (selector.Sel.Name == "Sprintf" || selector.Sel.Name == "Errorf")
}

func showingArgument(fun ast.Expr) (int, bool) {
	selector, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return 0, false
	}
	if qualifier, ok := selector.X.(*ast.Ident); ok {
		switch qualifier.Name + "." + selector.Sel.Name {
		case "contracts.DataResult", "fmt.Errorf", "errors.New":
			return 0, true
		case "contracts.Text":
			return 1, true
		}
	}
	return builderArgument(selector.Sel.Name)
}

func builderArgument(method string) (int, bool) {
	switch method {
	case "Help":
		return 0, true
	case "Param", "ValueParam":
		return 1, true
	}
	return 0, false
}

func (walker *sinkWalker) proseOf(expr ast.Expr) (finding, bool) {
	text, ok := walker.textOf(expr)
	position := walker.files.Position(expr.Pos())
	if !ok || !isProse(text) || isExempt(position, text) {
		return finding{}, false
	}
	return finding{position: position, text: text}, true
}

func (walker *sinkWalker) textOf(expr ast.Expr) (string, bool) {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		return unquote(typed)
	case *ast.Ident:
		return walker.constantText(typed.Name)
	case *ast.BinaryExpr:
		return walker.concatText(typed)
	case *ast.CallExpr:
		if text, ok := walker.formatText(typed); ok {
			return text, true
		}
		return walker.joinText(typed)
	}
	return "", false
}

func (walker *sinkWalker) joinText(call *ast.CallExpr) (string, bool) {
	if len(call.Args) != 2 || !isQualified(call.Fun, "strings", "Join") {
		return "", false
	}
	list, ok := call.Args[0].(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	separator, ok := walker.textOf(call.Args[1])
	if !ok {
		return "", false
	}
	parts := make([]string, 0, len(list.Elts))
	shown := false
	for _, element := range list.Elts {
		text, isText := walker.textOf(element)
		shown = shown || isText
		parts = append(parts, text)
	}
	return strings.Join(parts, separator), shown
}

func (walker *sinkWalker) constantText(name string) (string, bool) {
	value, declared := walker.constants[name]
	if !declared || walker.resolving[name] {
		return "", false
	}
	walker.resolving[name] = true
	defer delete(walker.resolving, name)
	return walker.textOf(value)
}

func (walker *sinkWalker) concatText(sum *ast.BinaryExpr) (string, bool) {
	if sum.Op != token.ADD {
		return "", false
	}
	left, leftIsText := walker.textOf(sum.X)
	right, rightIsText := walker.textOf(sum.Y)
	return left + right, leftIsText || rightIsText
}

func (walker *sinkWalker) formatText(call *ast.CallExpr) (string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return "", false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok || qualifier.Name != "fmt" || !slices.Contains([]string{"Sprintf", "Sprint", "Errorf"}, selector.Sel.Name) {
		return "", false
	}
	return walker.textOf(call.Args[0])
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
