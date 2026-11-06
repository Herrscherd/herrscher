package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type finding struct {
	position token.Position
	text     string
}

func presentationScope() []string {
	return []string{"plugins/terminal", "plugins/terminal/tui", "manage", cliDirectory, "core/host", "core/internal/manager", "core/internal/worktree", "usage.go"}
}

func collect(root string, scope []string) (map[string][]finding, error) {
	if err := validateExemptions(exemptions()); err != nil {
		return nil, err
	}
	files := token.NewFileSet()
	found := make(map[string][]finding)
	for _, entry := range scope {
		parsed, err := parseSources(files, filepath.Join(root, filepath.FromSlash(entry)))
		if err != nil {
			return nil, err
		}
		found[entry] = findPackageSinks(files, parsed)
	}
	return found, nil
}

func parseSources(files *token.FileSet, target string) ([]*ast.File, error) {
	paths, err := sourcePaths(target)
	if err != nil {
		return nil, err
	}
	var parsed []*ast.File
	for _, path := range paths {
		file, err := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %s: %w", path, err)
		}
		if !ast.IsGenerated(file) {
			parsed = append(parsed, file)
		}
	}
	return parsed, nil
}

func sourcePaths(target string) ([]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", target, err)
	}
	if !info.IsDir() {
		return productionSources([]string{target}), nil
	}
	var paths []string
	err = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != target {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", target, err)
	}
	return productionSources(paths), nil
}

func productionSources(paths []string) []string {
	var sources []string
	for _, path := range paths {
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			sources = append(sources, path)
		}
	}
	return sources
}
