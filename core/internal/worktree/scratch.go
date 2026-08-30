package worktree

import (
	"fmt"
	"os"
	"path/filepath"
)

const scratchDir = ".herrscher-scratch"

func (w *Worktreer) ScratchPath(root, name string) string {
	return scratchPathIn(root, w.instanceID, name)
}

func scratchPathIn(root, instanceID, name string) string {
	if instanceID == "" {
		return filepath.Join(root, scratchDir, name)
	}
	return filepath.Join(root, scratchDir, instanceID, name)
}

func (w *Worktreer) Scratch(root, name string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("scratch: no root to place session %q under", name)
	}
	if !validPathComponent(name) {
		return "", fmt.Errorf("scratch: refusing session name %q as a path component", name)
	}
	p := w.ScratchPath(root, name)
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", fmt.Errorf("scratch %q: %w", p, err)
	}
	return p, nil
}

func (w *Worktreer) RemoveScratch(root, name string) error {
	if root == "" || !validPathComponent(name) {
		return fmt.Errorf("scratch: refusing to remove session %q under root %q", name, root)
	}
	p := w.ScratchPath(root, name)
	if _, err := os.Lstat(p); os.IsNotExist(err) {
		return nil
	}
	if err := os.RemoveAll(p); err != nil {
		return fmt.Errorf("remove scratch %q: %w", p, err)
	}
	_ = os.Remove(filepath.Dir(p))
	return nil
}
