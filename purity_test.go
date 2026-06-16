package main

import (
	"go/build"
	"strings"
	"testing"
)

// TestHostPurity asserts the composition root (package main) never imports the
// concrete Discord adapter (dctl). The host drives chat platforms only through
// the contracts gateway port; dctl belongs to the discord-gateway plugin alone.
// (core/... is guarded separately by TestCorePurity.)
func TestHostPurity(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	const forbidden = "github.com/Herrscherd/dctl"
	imports := append(append([]string{}, pkg.Imports...), pkg.TestImports...)
	for _, imp := range imports {
		if imp == forbidden || strings.HasPrefix(imp, forbidden+"/") {
			t.Errorf("host package main imports forbidden %q", imp)
		}
	}
}
