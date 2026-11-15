//go:build !unix && !windows

package host

import (
	"os"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func tryLock(*os.File) (bool, error) { return true, nil }

func lockInstance(runtimedir.Layout, string) (func(), error) { return func() {}, nil }
