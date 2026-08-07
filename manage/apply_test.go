package manage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scriptedDecider answers both prompts from fixed values and records that it was
// asked, so a test can assert on the branch taken rather than on any output.
type scriptedDecider struct {
	warn, failed Decision
	warned       bool
	failedWith   string
}

func (d *scriptedDecider) Warn(ctx context.Context, findings []string) Decision {
	d.warned = true
	return d.warn
}

func (d *scriptedDecider) Failed(ctx context.Context, buildOutput string) Decision {
	d.failedWith = buildOutput
	return d.failed
}

// composition writes the three files apply saves, returning the directory.
func composition(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func read(t *testing.T, dir, name string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

// writeStep returns a step that overwrites one file in dir, standing in for what
// the Go toolchain would do.
func writeStep(dir, name, body string) step {
	return func(ctx context.Context) (string, error) {
		return "wrote " + name, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
	}
}

func original() map[string]string {
	return map[string]string{
		"go.mod":     "module m\n",
		"go.sum":     "h1:old\n",
		"plugins.go": "package main\n",
	}
}

func TestApplyAbortRunsNothing(t *testing.T) {
	dir := composition(t, original())
	d := &scriptedDecider{warn: Abort}
	ran := false
	steps := []step{func(ctx context.Context) (string, error) { ran = true; return "", nil }}

	if err := apply(context.Background(), dir, []string{"older than installed"}, d, steps); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !d.warned {
		t.Error("apply must ask before writing")
	}
	if ran {
		t.Error("a step ran after the operator aborted")
	}
	for name, want := range original() {
		if got, _ := read(t, dir, name); got != want {
			t.Errorf("%s = %q, want it untouched", name, got)
		}
	}
}

func TestApplySuccessKeepsWhatTheStepsWrote(t *testing.T) {
	dir := composition(t, original())
	d := &scriptedDecider{warn: Proceed}
	steps := []step{
		writeStep(dir, "go.mod", "module m\n\nrequire mod/a v1.0.0\n"),
		writeStep(dir, "go.sum", "h1:new\n"),
	}

	if err := apply(context.Background(), dir, nil, d, steps); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, _ := read(t, dir, "go.mod"); !strings.Contains(got, "mod/a v1.0.0") {
		t.Errorf("go.mod = %q, want the step's write to survive", got)
	}
	if got, _ := read(t, dir, "go.sum"); got != "h1:new\n" {
		t.Errorf("go.sum = %q, want the step's write to survive", got)
	}
	if d.failedWith != "" {
		t.Error("Failed must not be consulted when every step succeeds")
	}
}

func TestApplyRestoreUndoesEverything(t *testing.T) {
	dir := composition(t, original())
	d := &scriptedDecider{warn: Proceed, failed: Restore}
	third := false
	steps := []step{
		writeStep(dir, "go.mod", "module m\n\nrequire mod/a v1.0.0\n"),
		func(ctx context.Context) (string, error) {
			os.WriteFile(filepath.Join(dir, "plugins.go"), []byte("package main // broken\n"), 0o644)
			return "plugins.go:1: undefined: Register", errors.New("exit status 1")
		},
		func(ctx context.Context) (string, error) { third = true; return "", nil },
	}

	err := apply(context.Background(), dir, nil, d, steps)
	if err == nil {
		t.Fatal("want an error carrying the failing step's output")
	}
	if !strings.Contains(err.Error(), "undefined: Register") {
		t.Errorf("error = %v, want the compiler's own text", err)
	}
	if d.failedWith != "plugins.go:1: undefined: Register" {
		t.Errorf("Failed saw %q", d.failedWith)
	}
	if third {
		t.Error("a step after the failing one ran")
	}
	for name, want := range original() {
		if got, _ := read(t, dir, name); got != want {
			t.Errorf("%s = %q, want %q restored", name, got, want)
		}
	}
}

func TestApplyKeepLeavesTheTreeAlone(t *testing.T) {
	dir := composition(t, original())
	d := &scriptedDecider{warn: Proceed, failed: Keep}
	steps := []step{
		writeStep(dir, "go.mod", "module m\n\nrequire mod/a v1.0.0\n"),
		func(ctx context.Context) (string, error) { return "build failed", errors.New("exit status 1") },
	}

	err := apply(context.Background(), dir, nil, d, steps)
	if err == nil {
		t.Fatal("want an error even when the operator keeps the tree")
	}
	if got, _ := read(t, dir, "go.mod"); !strings.Contains(got, "mod/a v1.0.0") {
		t.Errorf("go.mod = %q, want the modified content kept for a hand repair", got)
	}
}

func TestApplyRestoreRemovesAFileThatDidNotExist(t *testing.T) {
	files := original()
	delete(files, "go.sum")
	dir := composition(t, files)
	d := &scriptedDecider{warn: Proceed, failed: Restore}
	steps := []step{
		writeStep(dir, "go.sum", "h1:created\n"),
		func(ctx context.Context) (string, error) { return "build failed", errors.New("exit status 1") },
	}

	if err := apply(context.Background(), dir, nil, d, steps); err == nil {
		t.Fatal("want an error")
	}
	if _, ok := read(t, dir, "go.sum"); ok {
		t.Error("go.sum survived a restore that should have removed it again")
	}
}

func TestApplyAutoDeciderProceedsThenRestores(t *testing.T) {
	dir := composition(t, original())
	steps := []step{
		writeStep(dir, "go.mod", "module m\n\nrequire mod/a v1.0.0\n"),
		func(ctx context.Context) (string, error) { return "build failed", errors.New("exit status 1") },
	}

	if err := apply(context.Background(), dir, []string{"older than installed"}, NewAutoDecider(), steps); err == nil {
		t.Fatal("want an error")
	}
	for name, want := range original() {
		if got, _ := read(t, dir, name); got != want {
			t.Errorf("%s = %q, want an unattended run to leave no broken tree", name, got)
		}
	}
}
