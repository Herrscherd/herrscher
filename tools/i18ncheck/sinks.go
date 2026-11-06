package main

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const (
	dynamicFormatText = "format passed as a parameter"
	dynamicJoinText   = "text joined with a value assigned at run time"
	maxPieces         = 64
)

type piece struct {
	text    string
	literal bool
	dynamic bool
}

type sinkWalker struct {
	files      *token.FileSet
	translated map[string]bool
	constants  map[string]ast.Expr
	locals     map[string][]ast.Expr
	resolving  map[string]bool
	exempt     map[ast.Node]bool
	found      []finding
}

func findPackageSinks(files *token.FileSet, parsed []*ast.File) []finding {
	walker := &sinkWalker{
		files:      files,
		translated: keyedSentinelNames(parsed),
		constants:  packageConstants(parsed),
		resolving:  make(map[string]bool),
		exempt:     make(map[ast.Node]bool),
	}
	for _, file := range parsed {
		for _, declaration := range file.Decls {
			walker.locals = localValues(declaration)
			ast.Inspect(declaration, walker.visit)
		}
	}
	return walker.found
}

func localValues(declaration ast.Decl) map[string][]ast.Expr {
	bindings := make(map[string][]ast.Expr)
	ast.Inspect(declaration, func(node ast.Node) bool {
		recordBindings(bindings, node)
		return true
	})
	return bindings
}

func recordBindings(bindings map[string][]ast.Expr, node ast.Node) {
	switch typed := node.(type) {
	case *ast.AssignStmt:
		recordAssignment(bindings, typed)
	case *ast.ValueSpec:
		recordValueSpec(bindings, typed)
	case *ast.RangeStmt:
		recordRangeTargets(bindings, typed)
	}
}

func recordAssignment(bindings map[string][]ast.Expr, assign *ast.AssignStmt) {
	pairs := (assign.Tok == token.DEFINE || assign.Tok == token.ASSIGN) && len(assign.Lhs) == len(assign.Rhs)
	for index, target := range assign.Lhs {
		ident, ok := target.(*ast.Ident)
		if !ok {
			continue
		}
		var value ast.Expr
		if pairs {
			value = assign.Rhs[index]
		}
		bindings[ident.Name] = append(bindings[ident.Name], value)
	}
}

func recordValueSpec(bindings map[string][]ast.Expr, spec *ast.ValueSpec) {
	for index, name := range spec.Names {
		var value ast.Expr
		if index < len(spec.Values) {
			value = spec.Values[index]
		}
		bindings[name.Name] = append(bindings[name.Name], value)
	}
}

func recordRangeTargets(bindings map[string][]ast.Expr, loop *ast.RangeStmt) {
	for _, target := range []ast.Expr{loop.Key, loop.Value} {
		if ident, ok := target.(*ast.Ident); ok {
			bindings[ident.Name] = append(bindings[ident.Name], nil)
		}
	}
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

func keyedSentinelNames(parsed []*ast.File) map[string]bool {
	translated := make(map[string]bool)
	for _, file := range parsed {
		ast.Inspect(file, func(node ast.Node) bool {
			if name, ok := keyedSentinelName(node); ok {
				translated[name] = true
			}
			return true
		})
	}
	return translated
}

func keyedSentinelName(node ast.Node) (string, bool) {
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
		found = append(found, walker.proseFindings(assign.Rhs[index])...)
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
		found = append(found, walker.proseFindings(pair.Value)...)
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
	var found []finding
	for _, shown := range walker.shownArguments(call) {
		found = append(found, walker.proseFindings(shown)...)
	}
	return found
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

func (walker *sinkWalker) shownArguments(call *ast.CallExpr) []ast.Expr {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	if qualifier, isIdent := selector.X.(*ast.Ident); isIdent {
		if shown, known := walker.shownByPackage(qualifier.Name+"."+selector.Sel.Name, call.Args); known {
			return shown
		}
	}
	index, ok := builderArgument(selector.Sel.Name)
	if !ok {
		return nil
	}
	return argumentAt(call.Args, index)
}

func (walker *sinkWalker) shownByPackage(function string, args []ast.Expr) ([]ast.Expr, bool) {
	switch function {
	case "contracts.DataResult", "fmt.Errorf", "errors.New":
		return argumentAt(args, 0), true
	case "contracts.Text":
		return argumentAt(args, 1), true
	case "fmt.Print", "fmt.Println", "log.Print", "log.Println":
		return args, true
	case "fmt.Printf", "log.Printf":
		return argumentAt(args, 0), true
	case "fmt.Fprint", "fmt.Fprintln":
		if !walker.writesToStandardStream(args) {
			return nil, true
		}
		return args[1:], true
	case "fmt.Fprintf":
		if !walker.writesToStandardStream(args) {
			return nil, true
		}
		return argumentAt(args[1:], 0), true
	}
	return nil, false
}

func (walker *sinkWalker) writesToStandardStream(args []ast.Expr) bool {
	return len(args) > 0 && walker.isStandardStream(args[0])
}

func (walker *sinkWalker) isStandardStream(expr ast.Expr) bool {
	switch typed := expr.(type) {
	case *ast.SelectorExpr:
		return isQualified(typed, "os", "Stdout") || isQualified(typed, "os", "Stderr")
	case *ast.Ident:
		return walker.nameIsStandardStream(typed.Name)
	}
	return false
}

func (walker *sinkWalker) nameIsStandardStream(name string) bool {
	if walker.resolving[name] {
		return false
	}
	values, isLocal := walker.locals[name]
	if !isLocal {
		value, isConstant := walker.constants[name]
		values = []ast.Expr{value}
		if !isConstant {
			return false
		}
	}
	walker.resolving[name] = true
	defer delete(walker.resolving, name)
	for _, value := range values {
		if value != nil && walker.isStandardStream(value) {
			return true
		}
	}
	return false
}

func argumentAt(args []ast.Expr, index int) []ast.Expr {
	if len(args) <= index {
		return nil
	}
	return args[index : index+1]
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

func (walker *sinkWalker) proseFindings(expr ast.Expr) []finding {
	switch typed := expr.(type) {
	case *ast.Ident:
		return walker.nameProseFindings(typed)
	case *ast.BinaryExpr:
		return walker.joinFindings(typed)
	}
	if sentence, ok := walker.proseOf(expr); ok {
		return []finding{sentence}
	}
	return nil
}

func (walker *sinkWalker) joinFindings(sum *ast.BinaryExpr) []finding {
	if sum.Op != token.ADD {
		return nil
	}
	position := walker.files.Position(sum.Pos())
	var found []finding
	partly := false
	for _, part := range walker.piecesOf(sum) {
		if part.dynamic {
			partly = partly || part.literal
			continue
		}
		if isProse(part.text) && !isExempt(position, part.text) {
			found = append(found, finding{position: position, text: part.text})
		}
	}
	if partly && len(found) == 0 {
		found = append(found, finding{position: position, text: dynamicJoinText})
	}
	return found
}

func (walker *sinkWalker) piecesOf(expr ast.Expr) []piece {
	switch typed := expr.(type) {
	case *ast.BinaryExpr:
		if typed.Op != token.ADD {
			return []piece{{}}
		}
		return joinPieces(walker.piecesOf(typed.X), walker.piecesOf(typed.Y))
	case *ast.Ident:
		return walker.namePieces(typed.Name)
	}
	if text, ok := walker.textOf(expr); ok {
		return []piece{{text: text, literal: isProse(text)}}
	}
	return []piece{{}}
}

func (walker *sinkWalker) namePieces(name string) []piece {
	if walker.resolving[name] {
		return []piece{{}}
	}
	walker.resolving[name] = true
	defer delete(walker.resolving, name)
	values, isLocal := walker.locals[name]
	if !isLocal {
		value, bound := walker.constants[name]
		if !bound || value == nil {
			return []piece{{}}
		}
		return walker.piecesOf(value)
	}
	var all []piece
	for _, value := range values {
		all = append(all, walker.assignedPieces(value)...)
	}
	return distinctPieces(all)
}

func (walker *sinkWalker) assignedPieces(value ast.Expr) []piece {
	switch value.(type) {
	case nil:
		return []piece{{dynamic: true}}
	case *ast.Ident, *ast.BinaryExpr:
		return walker.piecesOf(value)
	}
	if text, ok := walker.textOf(value); ok {
		return []piece{{text: text, literal: isProse(text)}}
	}
	return []piece{{dynamic: true}}
}

func joinPieces(left, right []piece) []piece {
	if len(left)*len(right) > maxPieces {
		return []piece{{}}
	}
	joined := make([]piece, 0, len(left)*len(right))
	for _, head := range left {
		for _, tail := range right {
			joined = append(joined, piece{text: head.text + tail.text, literal: head.literal || tail.literal, dynamic: head.dynamic || tail.dynamic})
		}
	}
	return distinctPieces(joined)
}

func distinctPieces(pieces []piece) []piece {
	seen := make(map[piece]bool, len(pieces))
	distinct := make([]piece, 0, len(pieces))
	for _, part := range pieces {
		if !seen[part] {
			seen[part] = true
			distinct = append(distinct, part)
		}
	}
	if len(distinct) > maxPieces {
		return []piece{{}}
	}
	return distinct
}

func (walker *sinkWalker) nameProseFindings(ident *ast.Ident) []finding {
	values, isLocal := walker.locals[ident.Name]
	if !isLocal {
		if sentence, ok := walker.proseOf(ident); ok {
			return []finding{sentence}
		}
		return nil
	}
	if walker.resolving[ident.Name] {
		return nil
	}
	walker.resolving[ident.Name] = true
	defer delete(walker.resolving, ident.Name)
	var found []finding
	for _, value := range values {
		if value != nil {
			found = append(found, walker.proseFindings(value)...)
		}
	}
	return found
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
		return walker.namedText(typed.Name)
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

func (walker *sinkWalker) namedText(name string) (string, bool) {
	if walker.resolving[name] {
		return "", false
	}
	if values, isLocal := walker.locals[name]; isLocal {
		return walker.singleValueText(name, values)
	}
	value, bound := walker.constants[name]
	if !bound || value == nil {
		return "", false
	}
	walker.resolving[name] = true
	defer delete(walker.resolving, name)
	return walker.textOf(value)
}

func (walker *sinkWalker) singleValueText(name string, values []ast.Expr) (string, bool) {
	if len(values) != 1 || values[0] == nil {
		return "", false
	}
	walker.resolving[name] = true
	defer delete(walker.resolving, name)
	return walker.textOf(values[0])
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
