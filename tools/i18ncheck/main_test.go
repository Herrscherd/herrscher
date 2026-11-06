package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	cleanRoot  = "testdata/clean"
	dirtyRoot  = "testdata/dirty"
	repository = "../.."
)

func runWith(t *testing.T, args ...string) (exit, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func writeBaseline(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baseline.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCleanFixtureNeedsNoBaseline(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runWith(t, "-baseline", writeBaseline(t, ""), cleanRoot)
	if code != exitClean || stdout != "" || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want a clean run", code, stdout, stderr)
	}
}

func TestDirtyFixtureMatchesItsExactCount(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runWith(t, "-baseline", writeBaseline(t, "manage 5\n"), dirtyRoot)
	if code != exitClean || stdout != "" || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want a clean run at the exact count", code, stdout, stderr)
	}
}

func TestNewSentenceAboveBaselineFails(t *testing.T) {
	t.Parallel()
	code, stdout, _ := runWith(t, "-baseline", writeBaseline(t, "manage 4\n"), dirtyRoot)
	if code != exitRatchet {
		t.Fatalf("exit %d, want %d", code, exitRatchet)
	}
	for _, want := range []string{
		`"Session stopped unexpectedly"`,
		`"Nothing was written to disk"`,
		"manage: 5 hard-coded sentences, the baseline allows 4",
		"move the new ones to the message catalog",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("report misses %q:\n%s", want, stdout)
		}
	}
}

func TestRemovedSentenceBelowBaselineFails(t *testing.T) {
	t.Parallel()
	code, stdout, _ := runWith(t, "-baseline", writeBaseline(t, "manage 6\n"), dirtyRoot)
	if code != exitRatchet {
		t.Fatalf("exit %d, want %d", code, exitRatchet)
	}
	if !strings.Contains(stdout, "manage: 5 hard-coded sentences left, the baseline still counts 6; lower its line in the baseline to 5") {
		t.Fatalf("report does not ask for the lower line:\n%s", stdout)
	}
}

func TestPackageAtZeroLosesItsLine(t *testing.T) {
	t.Parallel()
	code, stdout, _ := runWith(t, "-baseline", writeBaseline(t, "manage 5\ncore/cli 2\n"), dirtyRoot)
	if code != exitRatchet {
		t.Fatalf("exit %d, want %d", code, exitRatchet)
	}
	if !strings.Contains(stdout, "core/cli: no hard-coded sentence left, the baseline still counts 2; delete its line from the baseline") {
		t.Fatalf("report does not ask for the deleted line:\n%s", stdout)
	}
}

func TestZeroCountLineIsRefused(t *testing.T) {
	t.Parallel()
	code, _, stderr := runWith(t, "-baseline", writeBaseline(t, "manage 0\n"), dirtyRoot)
	if code != exitNoInput {
		t.Fatalf("exit %d, want %d", code, exitNoInput)
	}
	if !strings.Contains(stderr, "delete the line of a package at zero") {
		t.Fatalf("stderr does not explain the zero line:\n%s", stderr)
	}
}

func TestDuplicateBaselineLineIsRefused(t *testing.T) {
	t.Parallel()
	code, _, stderr := runWith(t, "-baseline", writeBaseline(t, "manage 5\nmanage 5\n"), dirtyRoot)
	if code != exitNoInput || !strings.Contains(stderr, "manage: package listed twice") {
		t.Fatalf("exit %d, stderr %q; want the duplicate refused", code, stderr)
	}
}

func TestMalformedBaselineLineIsRefused(t *testing.T) {
	t.Parallel()
	code, _, stderr := runWith(t, "-baseline", writeBaseline(t, "manage five\n"), dirtyRoot)
	if code != exitNoInput || !strings.Contains(stderr, "line 1") {
		t.Fatalf("exit %d, stderr %q; want the malformed line named", code, stderr)
	}
}

func TestMissingBaselineIsRefused(t *testing.T) {
	t.Parallel()
	code, _, stderr := runWith(t, "-baseline", filepath.Join(t.TempDir(), "absent.txt"), dirtyRoot)
	if code != exitNoInput || !strings.Contains(stderr, "cannot read baseline") {
		t.Fatalf("exit %d, stderr %q; want the missing baseline refused", code, stderr)
	}
}

func TestUsageErrors(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"no baseline": {dirtyRoot},
		"no root":     {"-baseline", writeBaseline(t, "")},
		"two roots":   {"-baseline", writeBaseline(t, ""), dirtyRoot, cleanRoot},
	} {
		code, _, stderr := runWith(t, args...)
		if code != exitUsage || !strings.Contains(stderr, "usage") {
			t.Errorf("%s: exit %d, stderr %q; want the usage exit", name, code, stderr)
		}
	}
}

func TestMutationAddedLiteralIsCaught(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	manage := filepath.Join(root, "manage")
	if err := os.Mkdir(manage, 0o700); err != nil {
		t.Fatal(err)
	}
	completeScope(t, root)
	mutant := "package manage\n\nfunc announce(m *model) {\n\tm.flash = \"Session stopped unexpectedly\"\n}\n"
	if err := os.WriteFile(filepath.Join(manage, "mutant.go"), []byte(mutant), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runWith(t, "-baseline", writeBaseline(t, ""), root)
	if code != exitRatchet || !strings.Contains(stdout, `"Session stopped unexpectedly"`) {
		t.Fatalf("exit %d, stdout %q; want the added literal reported", code, stdout)
	}
}

func TestRepositoryMatchesItsBaseline(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runWith(t, "-baseline", "baseline.txt", repository)
	if code != exitClean {
		t.Fatalf("exit %d, stdout %q, stderr %q; the repository must match baseline.txt", code, stdout, stderr)
	}
}

func TestProseRules(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]bool{
		"unknown kind":       true,
		"go list %s: %w: %s": true,
		"no host module found (looked for plugins.go)": true,
		"-v usage":                       true,
		"--yes":                          false,
		"/mode <manuel|edits|plan|auto>": false,
		"- Something went wrong here.":   true,
		"/ Something went wrong here.":   true,
		"< Something went wrong here.":   true,
		"-v":                             false,
		"tui.keys.reloaded":              false,
		"cancelled":                      true,
		"%w":                             false,
		"%q":                             false,
		"":                               false,
	} {
		if got := isProse(text); got != want {
			t.Errorf("isProse(%q) = %t, want %t", text, got, want)
		}
	}
}

func completeScope(t *testing.T, root string) {
	t.Helper()
	for _, entry := range presentationScope() {
		target := filepath.Join(root, filepath.FromSlash(entry))
		if strings.HasSuffix(entry, ".go") {
			if err := os.WriteFile(target, []byte("package main\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "clean.go"), []byte("package fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
