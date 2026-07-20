package core_test

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCorePurity asserts core/... never imports a concrete adapter (dctl) or any
// plugin. core is the agnostic engine: its dependency arrows point only at
// contracts and the standard library.
func TestCorePurity(t *testing.T) {
	forbidden := []string{
		"github.com/Herrscherd/dctl",
		"github.com/Herrscherd/herrscher-discord-gateway",
		"github.com/Herrscherd/herrscher-claude-backend",
	}
	roots := []string{
		"bridge", "config", "host", "service",
		"internal/control", "internal/forge", "internal/health",
		"internal/instanceid", "internal/manager", "internal/metrics",
		"internal/obs", "internal/state", "internal/supervisor",
		"internal/worktree",
	}
	for _, r := range roots {
		pkg, err := build.ImportDir(filepath.Join(".", r), 0)
		if err != nil {
			t.Fatalf("%s: %v", r, err)
		}
		imports := append(append([]string{}, pkg.Imports...), pkg.TestImports...)
		imports = append(imports, pkg.XTestImports...)
		for _, imp := range imports {
			for _, bad := range forbidden {
				if imp == bad || strings.HasPrefix(imp, bad+"/") {
					t.Errorf("core/%s imports forbidden %q", r, imp)
				}
			}
		}
	}
}

// TestCoreNamesNoConcretePlatform enforces the stronger contract the maintainer
// requires: the literal string "discord" must not appear ANYWHERE in core source
// — not in code, not in comments, not in test fixtures. The core is
// gateway-agnostic; a concrete platform's name is injected at the composition
// root (the herrscher binary), never hard-coded here. This guard walks every .go file
// under core/ (excluding itself, which necessarily names the forbidden token to
// forbid it) and fails on any case-insensitive match.
func TestCoreNamesNoConcretePlatform(t *testing.T) {
	const forbidden = "discord"
	self := "purity_test.go"
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || filepath.Base(path) == self {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(string(data)), forbidden) {
			t.Errorf("%s mentions %q — the core must never name a concrete platform", path, forbidden)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
