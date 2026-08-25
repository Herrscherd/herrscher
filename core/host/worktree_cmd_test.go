package host

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func tempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, argv := range [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "config", "user.email", "t@example.test"},
		{"git", "config", "user.name", "t"},
		{"git", "commit", "-q", "--allow-empty", "-m", "root"},
	} {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", argv, err, out)
		}
	}
	return dir
}

// captureStdout runs f with os.Stdout redirected and returns what it printed.
func captureStdout(t *testing.T, f func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	ferr := f()
	os.Stdout = old
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if ferr != nil {
		t.Fatalf("run: %v (printed %q)", ferr, buf.String())
	}
	return buf.String()
}

func TestWorktreeVerbCreatesAndRemoves(t *testing.T) {
	repo := tempRepo(t)
	ctx := context.Background()

	out := captureStdout(t, func() error {
		return RunWorktree(ctx, []string{"pre-existing", "--repo", repo, "--name", "s1", "--instance", "inst"})
	})
	var pre struct {
		PreExisting bool `json:"preExisting"`
	}
	if err := json.Unmarshal([]byte(out), &pre); err != nil {
		t.Fatalf("pre-existing output %q: %v", out, err)
	}
	if pre.PreExisting {
		t.Fatal("a fresh repo cannot have a pre-existing worktree")
	}

	out = captureStdout(t, func() error {
		return RunWorktree(ctx, []string{"create", "--repo", repo, "--name", "s1", "--instance", "inst"})
	})
	var made struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &made); err != nil {
		t.Fatalf("create output %q: %v", out, err)
	}
	want := filepath.Join(repo, ".herrscher-sessions", "inst", "s1")
	if made.Path != want {
		t.Fatalf("path = %q, want %q", made.Path, want)
	}
	if _, err := os.Stat(made.Path); err != nil {
		t.Fatalf("worktree not on disk: %v", err)
	}

	out = captureStdout(t, func() error {
		return RunWorktree(ctx, []string{"remove", "--repo", repo, "--name", "s1", "--instance", "inst", "--force"})
	})
	var gone struct {
		Removed bool `json:"removed"`
	}
	if err := json.Unmarshal([]byte(out), &gone); err != nil {
		t.Fatalf("remove output %q: %v", out, err)
	}
	if !gone.Removed {
		t.Fatal("remove reported nothing removed")
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("worktree still there: %v", err)
	}
}

func TestWorktreeVerbRejectsAnUnknownSubcommand(t *testing.T) {
	if err := RunWorktree(context.Background(), []string{"frobnicate", "--repo", "/x", "--name", "s"}); err == nil {
		t.Fatal("want an error for an unknown subcommand")
	}
}

func TestWorktreeVerbNeedsASubcommand(t *testing.T) {
	if err := RunWorktree(context.Background(), nil); err == nil {
		t.Fatal("want an error with no subcommand")
	}
}
