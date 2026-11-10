// Package leakguard fails a test run that leaves entries in the production
// runtime directory of the user running it.
package leakguard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

// Guard remembers the entries of the production directory before a test run.
type Guard struct {
	dir    string
	before []string
	err    error
}

// Start records the production directory entries before the tests run.
func Start() *Guard {
	dir := runtimedir.Production().Dir()
	before, err := entries(dir)
	return &Guard{dir: dir, before: before, err: err}
}

// Finish reports the entries the run added and returns the exit code to use:
// code, or 1 when the run added entries or the directory could not be read.
func (g *Guard) Finish(code int) int {
	if g.err != nil {
		fmt.Fprintf(os.Stderr, "leakguard: %v\n", g.err)
		return 1
	}
	after, err := entries(g.dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "leakguard: %v\n", err)
		return 1
	}
	var leaked []string
	for _, name := range after {
		if !slices.Contains(g.before, name) {
			leaked = append(leaked, filepath.Join(g.dir, name))
		}
	}
	if len(leaked) == 0 {
		return code
	}
	fmt.Fprintf(os.Stderr, "leakguard: the tests left entries in the production runtime directory:\n")
	for _, path := range leaked {
		fmt.Fprintf(os.Stderr, "  %s\n", path)
	}
	return 1
}

func entries(dir string) ([]string, error) {
	items, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name())
	}
	return names, nil
}
