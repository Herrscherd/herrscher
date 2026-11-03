package main

import (
	"go/ast"
	"go/build/constraint"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func strayComments(path string, file *ast.File, namedByAGenerator bool) []token.Pos {
	if namedByAGenerator && ast.IsGenerated(file) {
		return nil
	}
	allowed := allowedDocs(file)
	if strings.HasSuffix(path, "_test.go") {
		allowed = exampleDocs(file)
		allowExampleOutputs(file, allowed)
	}
	bodies := functionBodies(file)
	directory := filepath.Dir(path)
	var strays []token.Pos
	for _, group := range file.Comments {
		switch {
		case isInside(group, bodies):
			if !allowed[group] {
				strays = append(strays, group.Pos())
			}
		case allowed[group]:
			strays = append(strays, strayDirectiveLines(directory, group)...)
		case isManifestMarkerGroup(path, group):
		case !isDirective(directory, group):
			strays = append(strays, group.Pos())
		}
	}
	return strays
}

func isManifestMarkerGroup(path string, group *ast.CommentGroup) bool {
	if filepath.Base(path) != manifestFile || !isModuleRoot(filepath.Dir(path)) {
		return false
	}
	return !slices.ContainsFunc(group.List, func(line *ast.Comment) bool {
		return line.Text != manifestOpenMarker && line.Text != manifestCloseMarker
	})
}

func isModuleRoot(directory string) bool {
	info, err := os.Stat(filepath.Join(directory, "go.mod"))
	return err == nil && info.Mode().IsRegular()
}

func strayDirectiveLines(directory string, group *ast.CommentGroup) []token.Pos {
	var strays []token.Pos
	for _, line := range group.List {
		if strings.HasPrefix(line.Text, "//go:") && !isKnownDirective(directory, line.Text) {
			strays = append(strays, line.Slash)
		}
	}
	return strays
}

func functionBodies(file *ast.File) []*ast.BlockStmt {
	var bodies []*ast.BlockStmt
	ast.Inspect(file, func(node ast.Node) bool {
		switch declared := node.(type) {
		case *ast.FuncDecl:
			if declared.Body != nil {
				bodies = append(bodies, declared.Body)
			}
		case *ast.FuncLit:
			bodies = append(bodies, declared.Body)
		}
		return true
	})
	return bodies
}

func isInside(group *ast.CommentGroup, bodies []*ast.BlockStmt) bool {
	return slices.ContainsFunc(bodies, func(body *ast.BlockStmt) bool {
		return group.Pos() > body.Lbrace && group.End() < body.Rbrace
	})
}

func allowedDocs(file *ast.File) map[*ast.CommentGroup]bool {
	allowed := map[*ast.CommentGroup]bool{file.Doc: true}
	for _, declared := range file.Decls {
		switch top := declared.(type) {
		case *ast.FuncDecl:
			allowed[top.Doc] = top.Name.IsExported() && hasAnExportedReceiver(top)
		case *ast.GenDecl:
			allowed[top.Doc] = slices.ContainsFunc(top.Specs, isExportedSpec)
			for _, spec := range top.Specs {
				allowSpecDocs(spec, allowed)
			}
		}
	}
	delete(allowed, nil)
	return allowed
}

func hasAnExportedReceiver(function *ast.FuncDecl) bool {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return true
	}
	return isExportedTypeName(function.Recv.List[0].Type)
}

func allowSpecDocs(spec ast.Spec, allowed map[*ast.CommentGroup]bool) {
	switch declared := spec.(type) {
	case *ast.TypeSpec:
		allowed[declared.Doc] = declared.Name.IsExported()
		if declared.Name.IsExported() {
			allowFieldDocs(declared.Type, allowed)
		}
	case *ast.ValueSpec:
		exported := slices.ContainsFunc(declared.Names, (*ast.Ident).IsExported)
		allowed[declared.Doc] = exported
		if exported && declared.Type != nil {
			allowFieldDocs(declared.Type, allowed)
		}
	}
}

func allowFieldDocs(typeExpression ast.Expr, allowed map[*ast.CommentGroup]bool) {
	switch declared := typeExpression.(type) {
	case *ast.StructType:
		allowAccessibleFieldDocs(declared.Fields, allowed)
	case *ast.InterfaceType:
		allowAccessibleFieldDocs(declared.Methods, allowed)
	case *ast.StarExpr:
		allowFieldDocs(declared.X, allowed)
	case *ast.ArrayType:
		allowFieldDocs(declared.Elt, allowed)
	case *ast.MapType:
		allowFieldDocs(declared.Key, allowed)
		allowFieldDocs(declared.Value, allowed)
	case *ast.ChanType:
		allowFieldDocs(declared.Value, allowed)
	}
}

func allowAccessibleFieldDocs(fields *ast.FieldList, allowed map[*ast.CommentGroup]bool) {
	for _, field := range fields.List {
		accessible := isAccessibleField(field)
		allowed[field.Doc] = accessible
		if accessible {
			allowFieldDocs(field.Type, allowed)
		}
	}
}

func isAccessibleField(field *ast.Field) bool {
	if len(field.Names) > 0 {
		return slices.ContainsFunc(field.Names, (*ast.Ident).IsExported)
	}
	return isExportedTypeName(field.Type)
}

func isExportedTypeName(typeExpression ast.Expr) bool {
	for {
		switch unwrapped := typeExpression.(type) {
		case *ast.StarExpr:
			typeExpression = unwrapped.X
		case *ast.IndexExpr:
			typeExpression = unwrapped.X
		case *ast.IndexListExpr:
			typeExpression = unwrapped.X
		case *ast.SelectorExpr:
			return unwrapped.Sel.IsExported()
		case *ast.Ident:
			return unwrapped.IsExported()
		default:
			return false
		}
	}
}

func isExportedSpec(spec ast.Spec) bool {
	switch declared := spec.(type) {
	case *ast.TypeSpec:
		return declared.Name.IsExported()
	case *ast.ValueSpec:
		return slices.ContainsFunc(declared.Names, (*ast.Ident).IsExported)
	}
	return false
}

func isDirective(directory string, group *ast.CommentGroup) bool {
	return !slices.ContainsFunc(group.List, func(line *ast.Comment) bool {
		return !isKnownDirective(directory, line.Text)
	})
}

type directive struct {
	name      string
	arguments []string
}

func splitDirective(text string) directive {
	line, isGoDirective := strings.CutPrefix(text, "//go:")
	nameEnd := strings.IndexFunc(line, unicode.IsSpace)
	if nameEnd < 0 {
		nameEnd = len(line)
	}
	arguments, parsed := directiveWords(line[nameEnd:])
	if !isGoDirective || nameEnd == 0 || !parsed {
		return directive{}
	}
	return directive{name: line[:nameEnd], arguments: arguments}
}

func isKnownDirective(directory, text string) bool {
	found := splitDirective(text)
	arguments := found.arguments
	switch found.name {
	case "noinline", "nosplit", "noescape", "norace", "uintptrescapes":
		return len(arguments) == 0
	case "build":
		_, err := constraint.Parse(text)
		return err == nil
	case "generate":
		return isPinnedGenerator(directory, arguments)
	case "embed":
		return len(arguments) > 0 && !slices.ContainsFunc(arguments, func(pattern string) bool {
			return !matchesAFile(directory, pattern)
		})
	case "fix":
		return slices.Equal(arguments, []string{"inline"})
	case "debug":
		return len(arguments) == 1 && isSetting(arguments[0])
	}
	return false
}

func directiveWords(arguments string) ([]string, bool) {
	var patterns []string
	for rest := strings.TrimSpace(arguments); rest != ""; rest = strings.TrimSpace(rest) {
		end := strings.IndexFunc(rest, unicode.IsSpace)
		if quote := rest[0]; quote == '"' || quote == '`' {
			end = closingQuote(rest) + 1
		}
		if end <= 0 {
			end = len(rest)
		}
		pattern, err := unquotePattern(rest[:end])
		if err != nil {
			return nil, false
		}
		patterns = append(patterns, pattern)
		rest = rest[end:]
	}
	return patterns, true
}

func closingQuote(quoted string) int {
	for index := 1; index < len(quoted); index++ {
		switch {
		case quoted[index] == '\\' && quoted[0] == '"':
			index++
		case quoted[index] == quoted[0]:
			return index
		}
	}
	return -1
}

func unquotePattern(word string) (string, error) {
	if word[0] != '"' && word[0] != '`' {
		return word, nil
	}
	return strconv.Unquote(word)
}

func isPinnedGenerator(directory string, arguments []string) bool {
	if len(arguments) < 3 || arguments[0] != "go" || !isFlagList(arguments[3:]) {
		return false
	}
	switch command := arguments[2]; arguments[1] {
	case "run":
		return isVersionedModule(command) || isLocalPackage(directory, command)
	case toolDirective:
		return slices.Contains(declaredTools(directory), command)
	}
	return false
}

func isVersionedModule(command string) bool {
	module, version, versioned := strings.Cut(command, "@")
	return versioned && strings.Contains(module, ".") && !strings.HasPrefix(module, ".") && strings.HasPrefix(version, "v")
}

func isLocalPackage(directory, command string) bool {
	if command != "." && !strings.HasPrefix(command, "./") && !strings.HasPrefix(command, "../") {
		return false
	}
	info, err := os.Stat(filepath.Join(directory, command))
	return err == nil && info.IsDir()
}

func isFlagList(arguments []string) bool {
	for index := 0; index < len(arguments); index++ {
		if !strings.HasPrefix(arguments[index], "-") {
			return false
		}
		takesNextValue := !strings.Contains(arguments[index], "=")
		if takesNextValue && index+1 < len(arguments) && !strings.HasPrefix(arguments[index+1], "-") {
			index++
		}
	}
	return true
}

func declaredTools(directory string) []string {
	root, found := moduleRoot(directory)
	if !found {
		return nil
	}
	manifest, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil
	}
	var tools []string
	inToolBlock := false
	for line := range strings.Lines(string(manifest)) {
		words := strings.Fields(line)
		switch {
		case len(words) == 0:
		case inToolBlock && words[0] == ")":
			inToolBlock = false
		case inToolBlock:
			tools = append(tools, words[0], filepath.Base(words[0]))
		case words[0] == toolDirective && len(words) == 2 && words[1] == "(":
			inToolBlock = true
		case words[0] == toolDirective && len(words) == 2:
			tools = append(tools, words[1], filepath.Base(words[1]))
		}
	}
	return tools
}

func moduleRoot(directory string) (string, bool) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", false
	}
	for current := absolute; ; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, true
		}
		if filepath.Dir(current) == current {
			return "", false
		}
	}
}

func matchesAFile(directory, pattern string) bool {
	matches, err := filepath.Glob(filepath.Join(directory, strings.TrimPrefix(pattern, "all:")))
	return err == nil && len(matches) > 0
}

func isSetting(argument string) bool {
	key, value, hasValue := strings.Cut(argument, "=")
	return hasValue && key != "" && value != "" && !strings.ContainsFunc(key, func(letter rune) bool {
		return !unicode.IsLower(letter) && !unicode.IsDigit(letter)
	})
}

func allowExampleOutputs(file *ast.File, allowed map[*ast.CommentGroup]bool) {
	for _, declared := range file.Decls {
		example, isFunc := declared.(*ast.FuncDecl)
		if !isFunc || !isRunnableExample(example) {
			continue
		}
		if last := lastCommentIn(file, example.Body); last != nil && isExampleOutput(last) {
			allowed[last] = true
		}
	}
}

func isRunnableExample(example *ast.FuncDecl) bool {
	suffix, isExample := strings.CutPrefix(example.Name.Name, "Example")
	if !isExample || example.Recv != nil || example.Body == nil {
		return false
	}
	if example.Type.Params.NumFields() != 0 || example.Type.Results.NumFields() != 0 {
		return false
	}
	first, _ := utf8.DecodeRuneInString(suffix)
	return suffix == "" || first == '_' || unicode.IsUpper(first)
}

func lastCommentIn(file *ast.File, body *ast.BlockStmt) *ast.CommentGroup {
	var last *ast.CommentGroup
	for _, group := range file.Comments {
		if group.Pos() > body.Lbrace && group.End() < body.Rbrace {
			last = group
		}
	}
	return last
}

func isExampleOutput(group *ast.CommentGroup) bool {
	first := strings.TrimSpace(strings.TrimPrefix(group.List[0].Text, "//"))
	return strings.HasPrefix(first, "Output:") || strings.HasPrefix(first, "Unordered output:")
}

func exampleDocs(file *ast.File) map[*ast.CommentGroup]bool {
	allowed := make(map[*ast.CommentGroup]bool)
	for _, declared := range file.Decls {
		if example, isFunc := declared.(*ast.FuncDecl); isFunc && isRunnableExample(example) && example.Doc != nil {
			allowed[example.Doc] = true
		}
	}
	return allowed
}
