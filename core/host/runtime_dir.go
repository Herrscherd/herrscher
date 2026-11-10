package host

import (
	"errors"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

var errNoRuntimeLayout = errors.New("the daemon was started without a runtime directory")

// RuntimeFor is the per-user runtime layout of a daemon: the production layout
// when dir is empty, otherwise the layout rooted at dir, which must be an
// absolute path to a private directory.
func RuntimeFor(dir string) (runtimedir.Layout, error) {
	if dir == "" {
		return runtimedir.Production(), nil
	}
	return runtimedir.At(dir)
}

func ensureRuntimeDir(layout runtimedir.Layout, path string) error {
	return runtimeDirError(layout.Check(path))
}

func runtimeDirError(err error) error {
	unsafe, ok := errors.AsType[*runtimedir.UnsafeError](err)
	if !ok {
		return err
	}
	return wrapHost(err, runtimeDirKey(unsafe.Kind), map[string]any{pathParam: unsafe.Path, detailParam: unsafe.Reason})
}

func runtimeDirKey(kind runtimedir.Kind) string {
	switch kind {
	case runtimedir.KindSymlink:
		return "runtime.unsafe_dir.symlink"
	case runtimedir.KindForeignOwner:
		return "runtime.unsafe_dir.foreign_owner"
	case runtimedir.KindOpenAccess:
		return "runtime.unsafe_dir.open_access"
	case runtimedir.KindNotDir, runtimedir.KindOther:
		return "runtime.unsafe_dir"
	}
	return "runtime.unsafe_dir"
}
