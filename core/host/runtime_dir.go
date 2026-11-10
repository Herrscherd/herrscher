package host

import (
	"errors"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

var runtimeLayout = runtimedir.Production()

// SetRuntimeRoot moves the directory under which this process creates its sockets and locks.
func SetRuntimeRoot(root string) {
	runtimeLayout = runtimedir.Layout{Root: root}
}

func ensureRuntimeDir(path string) error {
	return runtimeDirError(runtimeLayout.Check(path))
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
