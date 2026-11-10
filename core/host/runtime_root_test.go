//go:build !windows

package host

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp(shortTempRoot, "rt")
	if err != nil {
		panic(err)
	}
	SetRuntimeRoot(root)
	code := m.Run()
	if err := os.RemoveAll(root); err != nil {
		panic(err)
	}
	os.Exit(code)
}
