//go:build !windows

package host

import (
	"os"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir/leakguard"
)

func TestMain(m *testing.M) {
	guard := leakguard.Start()
	os.Exit(guard.Finish(m.Run()))
}
