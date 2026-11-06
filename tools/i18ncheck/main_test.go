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

const mutantSentence = "Session stopped unexpectedly"

func runWith(t *testing.T, args ...string) (exit, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestCleanFixtureHasNoFinding(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runWith(t, cleanRoot)
	if code != exitClean || stdout != "" || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want a clean run", code, stdout, stderr)
	}
}

func TestDirtyFixtureReportsEverySentence(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runWith(t, dirtyRoot)
	if code != exitFindings || stderr != "" {
		t.Fatalf("exit %d, stderr %q; want the findings exit", code, stderr)
	}
	for _, want := range []string{
		`"Session stopped unexpectedly"`,
		`"Nothing was written to disk"`,
		"5 sentences shown to a person outside a typed exemption",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("report misses %q:\n%s", want, stdout)
		}
	}
}

func TestNewSentenceInEachScopePackageFails(t *testing.T) {
	t.Parallel()
	for _, entry := range presentationScope() {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			completeScope(t, root)
			target := filepath.Join(root, filepath.FromSlash(entry))
			if !strings.HasSuffix(entry, ".go") {
				target = filepath.Join(target, "mutant.go")
			}
			mutant := "package fixture\n\nimport \"errors\"\n\nfunc mutant() error {\n\treturn errors.New(\"" + mutantSentence + "\")\n}\n"
			if err := os.WriteFile(target, []byte(mutant), 0o600); err != nil {
				t.Fatal(err)
			}
			code, stdout, _ := runWith(t, root)
			if code != exitFindings || !strings.Contains(stdout, `"`+mutantSentence+`"`) {
				t.Fatalf("exit %d, stdout %q; want the new sentence in %s reported", code, stdout, entry)
			}
		})
	}
}

func TestUsageErrors(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"no root":         {},
		"two roots":       {dirtyRoot, cleanRoot},
		"legacy baseline": {"-baseline", "baseline.txt", cleanRoot},
	} {
		code, _, stderr := runWith(t, args...)
		if code != exitUsage || !strings.Contains(stderr, "usage") {
			t.Errorf("%s: exit %d, stderr %q; want the usage exit", name, code, stderr)
		}
	}
}

func TestRepositoryHasNoFinding(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runWith(t, repository)
	if code != exitClean {
		t.Fatalf("exit %d, stdout %q, stderr %q; the presentation packages must have no sentence outside a typed exemption", code, stdout, stderr)
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
