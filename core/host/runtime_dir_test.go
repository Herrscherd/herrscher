package host

import (
	"os"
	"testing"
)

func ensureTestRuntimeDir(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(runtimeLayout.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
}
