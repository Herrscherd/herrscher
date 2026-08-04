package skills

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Install materializes the skills carried inside src (one directory per skill)
// under dir, and reports the names it created. A skill that is already there is
// left alone: a playbook is a file rather than code precisely so it can be
// edited on the machine that runs the agent, and an upgrade that overwrote those
// edits would be worse than shipping no playbook at all.
func Install(src fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return nil, err
	}
	var made []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dest := filepath.Join(dir, e.Name())
		if _, err := os.Stat(dest); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return made, err
		}
		if err := copyTree(src, e.Name(), dest); err != nil {
			// A half-written skill would look installed and then never be
			// repaired, since Install skips whatever already exists.
			_ = os.RemoveAll(dest)
			return made, err
		}
		made = append(made, e.Name())
	}
	return made, nil
}

// copyTree writes the subtree rooted at root (slash paths, inside src) to dest.
func copyTree(src fs.FS, root, dest string) error {
	return fs.WalkDir(src, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(strings.TrimPrefix(p, root)))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
