package worktree

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const scratchDir = ".herrscher-scratch"

const scratchDirMode = 0o700

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
		return "", FailureError{Key: clitext.WorktreeScratchNoRoot, Params: clitext.Params{clitext.ParamName: strconv.Quote(name)}}
	}
	if !validPathComponent(name) {
		return "", FailureError{Key: clitext.WorktreeScratchBadName, Params: clitext.Params{clitext.ParamName: strconv.Quote(name)}}
	}
	scratchPath := w.ScratchPath(root, name)
	if err := os.MkdirAll(scratchPath, scratchDirMode); err != nil {
		return "", FailureError{Key: clitext.WorktreeScratchCreate, Params: clitext.Params{clitext.ParamPath: strconv.Quote(scratchPath), clitext.ParamErr: err}, Cause: err}
	}
	return scratchPath, nil
}

// RemoveScratch deletes the scratch directory for name under root.
func (w *Worktreer) RemoveScratch(root, name string) error {
	if root == "" || !validPathComponent(name) {
		return FailureError{Key: clitext.WorktreeScratchRemoveRefused, Params: clitext.Params{clitext.ParamName: strconv.Quote(name), clitext.ParamRoot: strconv.Quote(root)}}
	}
	scratchPath := w.ScratchPath(root, name)
	if _, err := os.Lstat(scratchPath); os.IsNotExist(err) {
		return nil
	}
	if err := os.RemoveAll(scratchPath); err != nil {
		return FailureError{Key: clitext.WorktreeScratchRemove, Params: clitext.Params{clitext.ParamPath: strconv.Quote(scratchPath), clitext.ParamErr: err}, Cause: err}
	}
	removeLogged(filepath.Dir(scratchPath))
	return nil
}
