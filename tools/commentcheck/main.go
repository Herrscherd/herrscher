// Command commentcheck refuses every Go comment that is not the doc comment of an exported identifier.
//
// It walks the directories it is given and reports each stray comment as
// file:line:column. Allowed are the package doc, the docs of exported
// package level identifiers, the known //go: directives in their exact form
// outside function bodies, the Output comment that closes a runnable example
// in a test file, every comment of a generated file that a
// //go:generate directive of its directory names as its output, and the two
// marker lines of the plugin manifest plugins.go at a module root.
//
// Usage:
//
//	commentcheck <dir>...
//
// Exit codes:
//
//	0   no stray comment
//	1   at least one stray comment was found and listed on stdout
//	2   the command line is wrong
//	66  a directory or a file cannot be read or parsed
//	74  the report or the diagnostic cannot be written
package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type exit uint8

const (
	exitClean   exit = 0
	exitStray   exit = 1
	exitUsage   exit = 2
	exitNoInput exit = 66
	exitIOError exit = 74
)

const toolDirective = "tool"

const (
	manifestFile        = "plugins.go"
	manifestOpenMarker  = "// herrscher:plugins"
	manifestCloseMarker = "// herrscher:end"
)

var errNoDirectory = errors.New("give at least one directory to check")

func main() {
	os.Exit(int(check(os.Args[1:], os.Stdout, os.Stderr)))
}

func check(dirs []string, stdout, stderr io.Writer) exit {
	if len(dirs) == 0 {
		return report(stderr, errNoDirectory, exitUsage)
	}
	var strays []string
	for _, dir := range dirs {
		found, err := straysUnder(dir)
		if err != nil {
			return report(stderr, err, exitNoInput)
		}
		strays = append(strays, found...)
	}
	if len(strays) == 0 {
		return exitClean
	}
	if _, err := fmt.Fprintln(stdout, strings.Join(strays, "\n")); err != nil {
		return report(stderr, err, exitIOError)
	}
	return exitStray
}

func report(stderr io.Writer, err error, code exit) exit {
	if _, writeErr := fmt.Fprintf(stderr, "commentcheck: %v\n", err); writeErr != nil {
		return exitIOError
	}
	return code
}

type parsedFile struct {
	path string
	file *ast.File
}

func straysUnder(root string) ([]string, error) {
	files := token.NewFileSet()
	byDirectory := make(map[string][]parsedFile)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".go" {
			return walkErr
		}
		file, err := parser.ParseFile(files, path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("cannot parse %s: %w", path, err)
		}
		directory := filepath.Dir(path)
		byDirectory[directory] = append(byDirectory[directory], parsedFile{path: path, file: file})
		return nil
	})
	if err != nil {
		return nil, err
	}
	var positions []token.Position
	for _, parsed := range byDirectory {
		outputs := generatorOutputs(parsed)
		for _, candidate := range parsed {
			namedByAGenerator := slices.Contains(outputs, filepath.Base(candidate.path))
			for _, stray := range strayComments(candidate.path, candidate.file, namedByAGenerator) {
				positions = append(positions, files.Position(stray))
			}
		}
	}
	slices.SortFunc(positions, comparePositions)
	strays := make([]string, 0, len(positions))
	for _, position := range positions {
		strays = append(strays, position.String())
	}
	return strays, nil
}

func comparePositions(first, second token.Position) int {
	if order := strings.Compare(first.Filename, second.Filename); order != 0 {
		return order
	}
	if first.Line != second.Line {
		return first.Line - second.Line
	}
	return first.Column - second.Column
}

func generatorOutputs(parsed []parsedFile) []string {
	var outputs []string
	for _, candidate := range parsed {
		if ast.IsGenerated(candidate.file) {
			continue
		}
		directory := filepath.Dir(candidate.path)
		for _, group := range candidate.file.Comments {
			for _, line := range group.List {
				if found := splitDirective(line.Text); found.name == "generate" && isKnownDirective(directory, line.Text) {
					outputs = append(outputs, outputFlagValues(found.arguments)...)
				}
			}
		}
	}
	return outputs
}

func outputFlagValues(arguments []string) []string {
	var outputs []string
	for index, argument := range arguments {
		flagName, value, hasValue := strings.Cut(argument, "=")
		if flagName != "-output" && flagName != "-o" {
			continue
		}
		if hasValue {
			outputs = append(outputs, value)
		} else if index+1 < len(arguments) {
			outputs = append(outputs, arguments[index+1])
		}
	}
	return outputs
}
