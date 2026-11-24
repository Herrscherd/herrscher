//go:build !unix && !windows

package host

import (
	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func lockInstance(*hostMessages, runtimedir.Layout, string) (func(), error) { return func() {}, nil }
