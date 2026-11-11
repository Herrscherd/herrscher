// Package runtimeguard fails a test run that changes the production runtime
// directory of the user running it. It lives outside core/internal so that the
// root package can use it as well as the host package.
package runtimeguard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"time"
)

// ErrNotDirectory reports that the watched runtime path exists but is not a directory.
var ErrNotDirectory = errors.New("runtime path is not a directory")

// Guard remembers the state of a runtime directory before a test run.
type Guard struct {
	dir    string
	before snapshot
	err    error
}

type snapshot struct {
	existed bool
	modTime time.Time
	entries map[string]fs.FileInfo
}

// Start records the state of dir before the tests run.
func Start(dir string) *Guard {
	before, err := takeSnapshot(dir)
	return &Guard{dir: dir, before: before, err: err}
}

// Changes lists what changed in the directory since Start, or why it could not be read.
func (g *Guard) Changes() ([]string, error) {
	if g.err != nil {
		return nil, g.err
	}
	after, err := takeSnapshot(g.dir)
	if err != nil {
		return nil, err
	}
	return g.before.diff(after), nil
}

// Finish returns code when the run changed nothing in the directory, and 1 when
// it did or when the directory could not be checked.
func (g *Guard) Finish(code int) int {
	changes, err := g.Changes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "runtimeguard: %v\n", err)
		return 1
	}
	if len(changes) == 0 {
		return code
	}
	fmt.Fprintf(os.Stderr, "runtimeguard: the tests changed the production runtime directory %s:\n", g.dir)
	for _, change := range changes {
		fmt.Fprintf(os.Stderr, "  %s\n", change)
	}
	return 1
}

func takeSnapshot(dir string) (snapshot, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return snapshot{}, nil
	}
	if err != nil {
		return snapshot{}, err
	}
	if !info.IsDir() {
		return snapshot{}, fmt.Errorf("%w: %s", ErrNotDirectory, dir)
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return snapshot{}, err
	}
	entries := make(map[string]fs.FileInfo, len(items))
	for _, item := range items {
		entry, infoErr := item.Info()
		if infoErr != nil {
			return snapshot{}, infoErr
		}
		entries[item.Name()] = entry
	}
	return snapshot{existed: true, modTime: info.ModTime(), entries: entries}, nil
}

func (s snapshot) diff(after snapshot) []string {
	var changes []string
	switch {
	case s.existed && !after.existed:
		changes = append(changes, "the directory was removed")
	case !s.existed && after.existed:
		changes = append(changes, "the directory was created")
	case s.existed && !s.modTime.Equal(after.modTime):
		changes = append(changes, fmt.Sprintf("the directory mtime moved from %s to %s",
			s.modTime.Format(time.RFC3339Nano), after.modTime.Format(time.RFC3339Nano)))
	}
	for _, name := range entryNames(s.entries, after.entries) {
		changes = append(changes, entryChanges(name, s.entries, after.entries)...)
	}
	return changes
}

func entryNames(before, after map[string]fs.FileInfo) []string {
	names := make([]string, 0, len(before)+len(after))
	for name := range before {
		names = append(names, name)
	}
	for name := range after {
		if _, seen := before[name]; !seen {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func entryChanges(name string, before, after map[string]fs.FileInfo) []string {
	old, existed := before[name]
	now, present := after[name]
	switch {
	case !existed:
		return []string{name + ": added"}
	case !present:
		return []string{name + ": removed"}
	}
	var changes []string
	if !os.SameFile(old, now) {
		changes = append(changes, name+": inode changed")
	}
	if old.Size() != now.Size() {
		changes = append(changes, fmt.Sprintf("%s: size changed from %d to %d", name, old.Size(), now.Size()))
	}
	if !old.ModTime().Equal(now.ModTime()) {
		changes = append(changes, name+": mtime changed")
	}
	return changes
}
