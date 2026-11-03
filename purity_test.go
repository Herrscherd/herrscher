package main

import (
	"go/build"
	"strings"
	"testing"
)

func TestHostPurity(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	const forbidden = "github.com/Herrscherd/dctl"
	imports := append(append([]string{}, pkg.Imports...), pkg.TestImports...)
	imports = append(imports, pkg.XTestImports...)
	for _, imp := range imports {
		if imp == forbidden || strings.HasPrefix(imp, forbidden+"/") {
			t.Errorf("host package main imports forbidden %q", imp)
		}
	}
}
