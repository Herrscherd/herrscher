package host

import (
	"errors"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func ensureRuntimeDir(path string) error {
	return runtimeDirError(runtimedir.Check(path))
}

func runtimeDirError(err error) error {
	unsafe, ok := errors.AsType[*runtimedir.UnsafeError](err)
	if !ok {
		return err
	}
	return wrapHost(err, "runtime.unsafe_dir", map[string]any{pathParam: unsafe.Path, "Detail": unsafe.Reason})
}
