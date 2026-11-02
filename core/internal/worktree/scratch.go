package worktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const scratchDir = ".herrscher-scratch"

const scratchDirMode = 0o700

var errScratchRefused = errors.New("refusing a scratch directory")

// ScratchPath returns the scratch directory path for name under root.
func (w *Worktreer) ScratchPath(root, name string) string {
	return scratchPathIn(root, w.instanceID, name)
}

func scratchPathIn(root, instanceID, name string) string {
	if instanceID == "" {
		return filepath.Join(root, scratchDir, name)
	}
	return filepath.Join(root, scratchDir, instanceID, name)
}

// Scratch creates the scratch directory for name under root.
func (w *Worktreer) Scratch(root, name string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("%w: no root to place session %q under", errScratchRefused, name)
	}
	if !validPathComponent(name) {
		return "", fmt.Errorf("%w: session name %q is not a path component", errScratchRefused, name)
	}
	scratchPath := w.ScratchPath(root, name)
	if err := os.MkdirAll(scratchPath, scratchDirMode); err != nil {
		return "", fmt.Errorf("scratch %q: %w", scratchPath, err)
	}
	return scratchPath, nil
}

// RemoveScratch deletes the scratch directory for name under root.
func (w *Worktreer) RemoveScratch(root, name string) error {
	if root == "" || !validPathComponent(name) {
		return fmt.Errorf("%w: to remove session %q under root %q", errScratchRefused, name, root)
	}
	scratchPath := w.ScratchPath(root, name)
	if _, err := os.Lstat(scratchPath); os.IsNotExist(err) {
		return nil
	}
	if err := os.RemoveAll(scratchPath); err != nil {
		return fmt.Errorf("remove scratch %q: %w", scratchPath, err)
	}
	removeLogged(filepath.Dir(scratchPath))
	return nil
}
