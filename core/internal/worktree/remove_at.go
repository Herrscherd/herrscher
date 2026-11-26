package worktree

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Herrscherd/herrscher/core/clitext"
)

// RemoveAt removes the worktree recorded at path, which must be a session
// directory of this instance inside repo. The recorded directory picks the
// worktree, not the session's current name, so a renamed session still removes
// the worktree it was created with. A path outside that root is refused, unless
// force is set and nothing is there any more.
func (w *Worktreer) RemoveAt(ctx context.Context, repo, path string, force bool) error {
	root := w.sessionsRoot(repo)
	if dir, ok := recordedChild(root, path); ok {
		return w.Remove(ctx, repo, dir, force)
	}
	if force && absent(path) {
		return nil
	}
	return outsideRoot(w.messages, path, root)
}

// RemoveScratchAt deletes the scratch directory recorded at path, which must be
// a scratch directory of this instance under root.
func (w *Worktreer) RemoveScratchAt(root, path string) error {
	scratchRoot := filepath.Join(root, scratchDir, w.instanceID)
	if dir, ok := recordedChild(scratchRoot, path); ok {
		return w.RemoveScratch(root, dir)
	}
	if absent(path) {
		return nil
	}
	return outsideRoot(w.messages, path, scratchRoot)
}

func (w *Worktreer) sessionsRoot(repo string) string {
	return filepath.Join(repo, sessionsDir, w.instanceID)
}

func recordedChild(root, path string) (string, bool) {
	dir := filepath.Base(path)
	if !validPathComponent(dir) || filepath.Clean(path) != filepath.Join(root, dir) {
		return "", false
	}
	return dir, true
}

func absent(path string) bool {
	_, err := os.Lstat(path)
	return errors.Is(err, fs.ErrNotExist)
}

func outsideRoot(messages *clitext.Messages, path, root string) error {
	return NewFailure(messages, clitext.WorktreeOutsideRoot, clitext.Params{clitext.ParamPath: strconv.Quote(path), clitext.ParamRoot: strconv.Quote(root)}, nil)
}
