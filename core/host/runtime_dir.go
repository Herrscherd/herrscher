package host

import (
	"errors"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

var errNoRuntimeLayout = errors.New("the daemon was started without a runtime directory")

// RuntimeLayout is the per-user directory where a daemon and its clients put
// their sockets and locks. Every function that reaches a socket takes one, so
// the caller decides which directory is in play.
type RuntimeLayout = runtimedir.Layout

// ProductionRuntime is the layout of the user running this process, the one a
// daemon serves and an operator client dials.
func ProductionRuntime() RuntimeLayout {
	return runtimedir.Production()
}

// RuntimeAt is the layout rooted at dir, an absolute path to a private directory.
func RuntimeAt(dir string) (RuntimeLayout, error) {
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
