package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/internal/errwriter"
)

const (
	reexecMarker       = "HERRSCHER_TEST_REEXEC"
	reexecAnnounceKey  = "0123456789abcdef0123456789abcdef"
	testRuntimeRootKey = "HERRSCHER_TEST_RUNTIME_ROOT"
)

func TestMain(m *testing.M) {
	switch os.Getenv(reexecMarker) {
	case "1":
		os.Exit(runStartupProbe())
	case "serve":
		os.Args = []string{"herrscher", "serve", "--env-file", os.Getenv("HERRSCHER_ENV_FILE"), "--runtime-dir", os.Getenv(testRuntimeRootKey)}
		os.Exit(run())
	}
	os.Exit(m.Run())
}

func runStartupProbe() int {
	os.Args = []string{"herrscher", "help"}
	code := run()
	_, present := os.LookupEnv("HERRSCHER_ANNOUNCE_KEY")
	captured := host.AnnounceKeySecret() == reexecAnnounceKey
	out := errwriter.New(os.Stdout)
	out.Printf("\nstartup probe present=%t captured=%t\n", present, captured)
	return code
}

func TestRunHidesTheAnnounceKeyFromTheEnvironmentAfterCapturingIt(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "herrscher.env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(t.Context(), os.Args[0])
	child.Env = append(os.Environ(),
		reexecMarker+"=1",
		"HERRSCHER_ANNOUNCE_KEY="+reexecAnnounceKey,
		"HERRSCHER_ENV_FILE="+envFile,
	)

	out, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("probe child failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "startup probe present=false captured=true") {
		t.Fatalf("the announce key must leave the environment and stay captured after run():\n%s", out)
	}
}
