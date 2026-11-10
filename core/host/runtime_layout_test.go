//go:build !windows

package host

import (
	"os"
	"path"
	"strconv"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

const shortTempRoot = "/tmp"

func isolatedRuntimeDir(t *testing.T) runtimedir.Layout {
	t.Helper()
	root := path.Join(shortTempRoot, "rt"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	previous := runtimeLayout
	runtimeLayout = runtimedir.Layout{Root: root}
	t.Cleanup(func() {
		runtimeLayout = previous
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	return runtimeLayout
}
