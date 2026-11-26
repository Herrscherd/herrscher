package manage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/internal/errwriter"
)

func hostCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, manifestFile), []byte(sample), modeFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.example"), []byte("CLAUDE_CMD=claude\n"), modeFile); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runSetupHost(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := setupHost(context.Background(), testMessages(), nil, errwriter.New(&out), errwriter.New(&errOut), args)
	return code, errOut.String()
}

func readManifest(t *testing.T, dir string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

func TestSetupYesComposesTheStackWithoutATerminal(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := hostCheckout(t)
	if code, stderr := runSetupHost(t, "--yes", "--no-build", "--no-trial", "--backend", kindCodex, "--host", dir); code != 0 {
		t.Fatalf("setup --yes exited %d: %s", code, stderr)
	}
	manifest := readManifest(t, dir)
	if !strings.Contains(manifest, catalog()[catBackend][kindCodex]) || strings.Contains(manifest, catalog()[catBackend][kindClaude]) {
		t.Fatalf("plugins.go does not hold the chosen backend:\n%s", manifest)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		t.Fatalf(".env not seeded: %v", err)
	}
}

func TestSetupStackRefusesASectionAndWritesNothing(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := hostCheckout(t)
	code, stderr := runSetupHost(t, "--yes", "--no-build", "--no-trial", "--host", dir, kindClaude)
	if code != exitUsage || !strings.Contains(stderr, kindClaude) {
		t.Fatalf("setup --yes claude exited %d with %q, want a usage error naming the section", code, stderr)
	}
	if readManifest(t, dir) != sample {
		t.Fatal("plugins.go was rewritten although the command was refused")
	}
}

func TestSetupStackOnlyFlagNeedsYes(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, flag := range []string{"--no-build", "--no-trial"} {
		code, stderr := runSetupHost(t, flag)
		if code != exitUsage || !strings.Contains(stderr, flag) {
			t.Errorf("setup %s exited %d with %q, want a usage error naming the flag", flag, code, stderr)
		}
	}
}

func TestSetupWithoutATerminalPointsAtYes(t *testing.T) {
	t.Chdir(t.TempDir())
	code, stderr := runSetupHost(t)
	if code != 1 || !strings.Contains(stderr, "--yes") {
		t.Fatalf("setup without a terminal exited %d with %q, want a failure pointing at --yes", code, stderr)
	}
}

func TestSetupExplicitHostWithoutManifestIsRefused(t *testing.T) {
	cwd := hostCheckout(t)
	t.Chdir(cwd)
	missing := t.TempDir()
	code, stderr := runSetupHost(t, "--yes", "--no-build", "--no-trial", "--host", missing)
	if code != exitUsage || !strings.Contains(stderr, missing) {
		t.Fatalf("setup --host %s exited %d with %q, want a usage error naming the path", missing, code, stderr)
	}
	if readManifest(t, cwd) != sample {
		t.Fatal("the checkout in the current directory was rewritten instead of the one named by --host")
	}
}

func TestResolveHostNeverFallsBackFromAnExplicitPath(t *testing.T) {
	t.Chdir(hostCheckout(t))
	if _, err := resolveHost(testMessages(), t.TempDir()); !errors.Is(err, errNoHostModule) {
		t.Fatalf("resolveHost with an explicit path lacking plugins.go returned %v, want errNoHostModule", err)
	}
	if dir, err := resolveHost(testMessages(), ""); err != nil || dir != "." {
		t.Fatalf("resolveHost without --host returned %q, %v, want the current directory", dir, err)
	}
}
