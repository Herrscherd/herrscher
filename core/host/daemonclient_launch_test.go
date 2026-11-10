package host

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestLaunchProjectNamesTheRepositoryUnderTheWorkspace(t *testing.T) {
	workspace := t.TempDir()
	repo := filepath.Join(workspace, "herrscher")
	nested := filepath.Join(repo, "core", "host")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(context.Background(), "git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	outside := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{"workspace":`+strconv.Quote(workspace)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, dir, want string
	}{
		{name: "repository root", dir: repo, want: "herrscher"},
		{name: "inside the repository", dir: nested, want: "herrscher"},
		{name: "the workspace itself", dir: workspace, want: ""},
		{name: "outside the workspace", dir: outside, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LaunchProject(context.Background(), statePath, tc.dir); got != tc.want {
				t.Fatalf("LaunchProject(%s) = %q, want %q", tc.dir, got, tc.want)
			}
		})
	}
}

func TestLaunchProjectWithoutAWorkspaceKeepsTheDaemonPlacement(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if got := LaunchProject(context.Background(), statePath, t.TempDir()); got != "" {
		t.Fatalf("LaunchProject with no state = %q, want empty", got)
	}
}
