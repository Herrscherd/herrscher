//go:build unix

package main

import (
	"os"
	"testing"

	"github.com/Herrscherd/herrscher/core/host"
)

const shortTempRoot = "/tmp"

func runWithRuntimeRoot(m *testing.M) int {
	root, err := os.MkdirTemp(shortTempRoot, "rt")
	if err != nil {
		panic(err)
	}
	host.SetRuntimeRoot(root)
	code := m.Run()
	if err := os.RemoveAll(root); err != nil {
		panic(err)
	}
	return code
}
