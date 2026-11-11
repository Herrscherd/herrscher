package productionlayout

import (
	"go/parser"
	"go/token"
	"testing"
)

func fixture(t *testing.T, rel, src string) sourceFile {
	t.Helper()
	fset := token.NewFileSet()
	tree, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse fixture %s: %v", rel, err)
	}
	return sourceFile{rel: rel, fset: fset, tree: tree}
}

func expectViolations(t *testing.T, got []string, want int) {
	t.Helper()
	if len(got) != want {
		t.Fatalf("got %d violations, want %d: %q", len(got), want, got)
	}
}

func TestRegressionProductionHelperCallingTheWrapperIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/helper.go", `package host

func helperSocket() string { return CommandSocketPath("instance-1") }
`)}
	expectViolations(t, productionWrapperViolations(files), 1)
}

func TestRegressionWrapperFileMayNameTheWrappers(t *testing.T) {
	files := []sourceFile{fixture(t, unixWrappersFile, `package host

func CommandSocketPath(id string) string { return EventsSocketPath(id) }
`)}
	expectViolations(t, productionWrapperViolations(files), 0)
}

func TestRegressionTestCallingTheWrapperIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/socket_test.go", `package host

import "testing"

func TestSocket(t *testing.T) { _ = CommandSocketPath("instance-1") }
`)}
	expectViolations(t, testWrapperViolations(files), 1)
}

func TestRegressionHiddenDirectoriesAreWalked(t *testing.T) {
	if skippedDir("../../.probe", ".probe") {
		t.Fatal("a hidden directory is skipped, so a Go file inside it escapes every rule")
	}
	if !skippedDir("../../.git", gitDirName) {
		t.Fatal("the git metadata directory is walked")
	}
	files := []sourceFile{fixture(t, ".probe/probe.go", `package probe

var _ = ProductionRuntime
`)}
	expectViolations(t, productionRuntimeViolations(files), 1)
}

func TestRegressionProductionLayoutInATestFileIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/layout_test.go", `package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func TestLayout(t *testing.T) { _ = runtimedir.Production() }
`)}
	expectViolations(t, productionLayoutViolations(files), 1)
}

func TestRegressionRootsLiteralIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/roots_test.go", `package host

import rt "github.com/Herrscherd/herrscher/core/internal/runtimedir"

var _ = rt.Roots{RunUser: "/run/user", Tmp: "/tmp"}
`)}
	expectViolations(t, runtimedirSurfaceViolations(files), 1)
}

func TestRegressionResolveWithProductionRootsIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/resolve_test.go", `package host

import (
	"os"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func TestResolve(t *testing.T) {
	_, _ = runtimedir.Resolve(os.Getuid(), runtimedir.ProductionRoots())
}
`)}
	expectViolations(t, runtimedirSurfaceViolations(files), 2)
}

func TestRegressionDotImportOfTheRuntimeDirectoryIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/dot_test.go", `package host

import . "github.com/Herrscherd/herrscher/core/internal/runtimedir"

var _ = Roots{RunUser: "/run/user", Tmp: "/tmp"}
`)}
	expectViolations(t, runtimedirSurfaceViolations(files), 1)
}

func TestRegressionUseOfTheRuntimeDirectoryWithoutItsForbiddenNamesIsAllowed(t *testing.T) {
	files := []sourceFile{
		fixture(t, "core/host/ok.go", `package host

import "github.com/Herrscherd/herrscher/core/internal/runtimedir"

var _ = runtimedir.At
`),
		fixture(t, "core/other/roots.go", `package other

type Roots struct{}

var _ = Roots{}
`),
	}
	expectViolations(t, runtimedirSurfaceViolations(files), 0)
}

func TestRegressionDirOfProbeIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/probe_test.go", `package host

import (
	"os"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

var _ = runtimedir.At(runtimedir.DirOf("/tmp", os.Getuid()))
`)}
	expectViolations(t, runtimedirSurfaceViolations(files), 1)
}

func TestRegressionProductionLayoutFromAnArbitraryPathIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/stale.go", `package host

import "github.com/Herrscherd/herrscher/core/internal/runtimedir"

func stale() { _, _ = runtimedir.At("/tmp/herrscher-1000") }
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 1)
}

func TestRegressionTestLayoutFromAnArbitraryPathIsAllowed(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/stale_test.go", `package host

import "github.com/Herrscherd/herrscher/core/internal/runtimedir"

func stale() { _, _ = runtimedir.At("/tmp/herrscher-1000") }
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 0)
}

func TestRegressionQualifiedRuntimeAtInAnotherPackageIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/outbox/stale.go", `package outbox

import "github.com/Herrscherd/herrscher/core/host"

func stale() { _, _ = host.RuntimeAt("/tmp/herrscher-1000") }
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 1)
}

func TestRegressionUnqualifiedRuntimeAtInHostIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/stale.go", `package host

func stale() { _, _ = RuntimeAt("/tmp/herrscher-1000") }
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 1)
}

func TestRegressionAliasedRuntimedirAtIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/aliased.go", `package host

import rt "github.com/Herrscherd/herrscher/core/internal/runtimedir"

func aliased() { _, _ = rt.At("/tmp/herrscher-1000") }
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 1)
}

func TestRegressionDotImportedRuntimedirAtIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/dotted.go", `package host

import . "github.com/Herrscherd/herrscher/core/internal/runtimedir"

func dotted() { _, _ = At("/tmp/herrscher-1000") }
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 1)
}

func TestRegressionAliasedHostRuntimeAtIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/outbox/aliased.go", `package outbox

import h "github.com/Herrscherd/herrscher/core/host"

func aliased() { _, _ = h.RuntimeAt("/tmp/herrscher-1000") }
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 1)
}

func TestRegressionLocalNameRuntimedirOfAnotherPackageIsAllowed(t *testing.T) {
	files := []sourceFile{fixture(t, "core/outbox/other.go", `package outbox

import runtimedir "example.com/other/runtimedir"

func other() { _, _ = runtimedir.At("/tmp/herrscher-1000") }
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 0)
}

func TestRegressionUncalledRuntimedirAtReferenceIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/build.go", `package host

import "github.com/Herrscherd/herrscher/core/internal/runtimedir"

var build = runtimedir.At
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 1)
}

func TestRegressionUncalledQualifiedRuntimeAtIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/outbox/build.go", `package outbox

import "github.com/Herrscherd/herrscher/core/host"

var build = host.RuntimeAt
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 1)
}

func TestRegressionUncalledBareRuntimeAtInHostIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/build.go", `package host

var build = RuntimeAt
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 1)
}

func TestRegressionUncalledDotImportedRuntimedirAtIsFlagged(t *testing.T) {
	files := []sourceFile{fixture(t, "core/host/dotbuild.go", `package host

import . "github.com/Herrscherd/herrscher/core/internal/runtimedir"

var build = At
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 1)
}

func TestRegressionBareRuntimeAtOutsideHostIsAllowed(t *testing.T) {
	files := []sourceFile{fixture(t, "core/outbox/local.go", `package outbox

func RuntimeAt(dir string) string { return dir }

var build = RuntimeAt
`)}
	expectViolations(t, arbitraryLayoutViolations(files), 0)
}
