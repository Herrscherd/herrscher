//go:build !unix

package main

import "testing"

func runWithRuntimeRoot(m *testing.M) int {
	return m.Run()
}
