package forgecreds

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func installFakeGH(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake gh is a POSIX shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, gitHubCLI), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestGitHubCLITokenReadsOnlyStandardOutput(t *testing.T) {
	installFakeGH(t, `if [ "$1" = auth ] && [ "$2" = token ] && [ "$3" = --hostname ] && [ "$4" = github.com ]; then
  echo "warning: unrelated text" >&2
  echo "gho_from_gh"
  exit 0
fi
exit 1`)

	token, err := gitHubCLIToken(t.Context(), mustHost(t, "github.com"))
	if err != nil {
		t.Fatalf("gitHubCLIToken: %v", err)
	}
	if token != "gho_from_gh" {
		t.Fatalf("gitHubCLIToken = %q, want only the token line", token)
	}
}

func TestGitHubCLIFailureGivesNoToken(t *testing.T) {
	installFakeGH(t, `echo "not logged in" >&2
exit 1`)

	token, err := gitHubCLIToken(t.Context(), mustHost(t, "github.com"))
	if err == nil || token != "" {
		t.Fatalf("gitHubCLIToken after a failed login = %q, %v; want no token and an error", token, err)
	}
}

func TestGitHubCLIOutputBeyondTheBoundIsRefused(t *testing.T) {
	installFakeGH(t, `head -c 10000 /dev/zero | tr '\0' 'a'
exit 0`)

	token, err := gitHubCLIToken(t.Context(), mustHost(t, "github.com"))
	if err == nil || token != "" {
		t.Fatalf("gitHubCLIToken with oversized output = %d bytes, %v; want refusal", len(token), err)
	}
	if strings.Contains(err.Error(), "aaaa") {
		t.Fatalf("the error echoes the output")
	}
}

func TestMissingGHGivesNoToken(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	token, err := gitHubCLIToken(t.Context(), mustHost(t, "github.com"))
	if err == nil || token != "" {
		t.Fatalf("gitHubCLIToken without gh = %q, %v; want no token and an error", token, err)
	}
}

func TestGitHubCLIFailureNeverEchoesAPrintedToken(t *testing.T) {
	const leaked = "gho_PRINTED_THEN_FAILED"
	installFakeGH(t, `echo "`+leaked+`"
exit 1`)

	token, err := gitHubCLIToken(t.Context(), mustHost(t, "github.com"))
	if err == nil || token != "" {
		t.Fatalf("gitHubCLIToken after a failure = %q, %v; want no token and an error", token, err)
	}
	if strings.Contains(err.Error(), leaked) {
		t.Fatalf("the error %q carries the token printed before the failure", err)
	}
}
