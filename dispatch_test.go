package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/host"
)

func expectedLocalVerbs() []string {
	return []string{
		"init", "plugin", "update", "install",
		"bridge", "serve", "session", "agent", "memory", "models", "service", "plugin-host",
		"-h", "--help", "help",
		"approve",
	}
}

func mainSwitches(t *testing.T) ([]string, bool) {
	t.Helper()
	f := mainFile(t)
	var cases []string
	ast.Inspect(f, func(n ast.Node) bool {
		if cl, ok := n.(*ast.CaseClause); ok {
			cases = append(cases, stringLabels(cl.List)...)
		}
		return true
	})
	return cases, defaultArmForwards(t, f)
}

func mainFile(t *testing.T) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func funcNamed(t *testing.T, f *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == name && fn.Recv == nil {
			return fn
		}
	}
	t.Fatalf("no %s() in main.go", name)
	return nil
}

func defaultArmForwards(t *testing.T, f *ast.File) bool {
	t.Helper()
	return mentionsIdent(funcNamed(t, f, "runCommand"), "runForwarded") &&
		mentionsIdent(funcNamed(t, f, "runForwarded"), "forwardUnknownVerb")
}

func mentionsIdent(n ast.Node, name string) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return true
	})
	return found
}

func stringLabels(exprs []ast.Expr) []string {
	var labels []string
	for _, e := range exprs {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			labels = append(labels, lit.Value[1:len(lit.Value)-1])
		}
	}
	return labels
}

func TestKnownVerbsKeepTheirLocalPath(t *testing.T) {
	cases, _ := mainSwitches(t)
	seen := make(map[string]bool)
	for _, c := range cases {
		seen[c] = true
	}
	for _, v := range expectedLocalVerbs() {
		if !seen[v] {
			t.Errorf("verb %q lost its own case in main(): it would now forward to the daemon", v)
		}
	}
}

func TestOnlyTheDefaultArmForwards(t *testing.T) {
	_, defaultForwards := mainSwitches(t)
	if !defaultForwards {
		t.Fatal("main()'s default arm does not forward the argv to the daemon")
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "forwardUnknownVerb" {
			calls++
		}
		return true
	})
	if calls != 1 {
		t.Fatalf("forwardUnknownVerb referenced %d times in main.go, want 1 (the default arm only)", calls)
	}
}

func swapStdio(t *testing.T, payload string) func() (string, string) {
	t.Helper()
	pipe := func() (*os.File, *os.File) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		return r, w
	}
	inR, inW := pipe()
	outR, outW := pipe()
	errR, errW := pipe()
	go func() {
		if _, err := inW.WriteString(payload); err != nil {
			t.Errorf("write payload: %v", err)
		}
		closeQuietly(inW)
	}()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = inR, outW, errW
	t.Cleanup(func() {
		os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
		closeQuietly(inR)
		closeQuietly(outR)
		closeQuietly(errR)
	})
	return func() (string, string) {
		closeQuietly(outW)
		closeQuietly(errW)
		out, err := io.ReadAll(outR)
		if err != nil {
			t.Errorf("read stdout: %v", err)
		}
		errs, err := io.ReadAll(errR)
		if err != nil {
			t.Errorf("read stderr: %v", err)
		}
		return string(out), string(errs)
	}
}

func TestApproveHookAnswersWithoutBuildingTheRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERRSCHER_STATE_DIR", dir)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("HERRSCHER_COMMAND_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
	t.Setenv(host.SessionVar, "s1")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runApprove(context.Background(), []string{"list"}); err == nil {
		t.Fatal("`approve list` built a registry out of an unparseable config.json")
	}

	read := swapStdio(t, `{"tool_name":"Bash","tool_input":{"command":"ls"}}`)
	err := runApprove(context.Background(), []string{"hook"})
	out, errOut := read()
	if err != nil {
		t.Fatalf("the hook failed, which the vendor CLI reads as a crash: %v", err)
	}
	if !strings.Contains(out, `"permissionDecision":"allow"`) {
		t.Fatalf("hook wrote %q, want an allow", out)
	}
	if !strings.Contains(errOut, "no daemon is listening") {
		t.Fatalf("stderr was %q, want the note that says why the guardrail is not running", errOut)
	}
}

func TestForwardUnknownVerbWithoutDaemon(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	t.Setenv("HERRSCHER_INSTANCE_ID", "dispatch-test")

	out, handled, err := forwardUnknownVerb(context.Background(), []string{"whatever", "list"})
	if handled {
		t.Fatal("handled = true, want false with no daemon listening")
	}
	if err != nil || out != "" {
		t.Fatalf("forwardUnknownVerb = (%q, %v), want empty and no error", out, err)
	}
}

type stubForward struct {
	argv    []string
	out     string
	handled bool
	err     error
}

func (s *stubForward) fwd(_ context.Context, argv []string) (string, bool, error) {
	s.argv = append([]string(nil), argv...)
	return s.out, s.handled, s.err
}

func TestDispatchUnknownForwardsTheWholeArgv(t *testing.T) {
	s := &stubForward{out: "two messages", handled: true}
	args := []string{"channel", "read", "--limit", "5"}
	out, exit, err := dispatchUnknown(context.Background(), "some-kind", args, s.fwd)

	want := []string{"some-kind", "channel", "read", "--limit", "5"}
	if !slices.Equal(s.argv, want) {
		t.Fatalf("forwarded argv = %v, want %v", s.argv, want)
	}
	if out != "two messages" || err != nil || exit != 0 {
		t.Fatalf("dispatchUnknown = (%q, %v, %d), want (\"two messages\", nil, 0)", out, err, exit)
	}
}

func TestDispatchUnknownKeepsTheDaemonsFailure(t *testing.T) {
	boom := errors.New(`plugin: channel "nope" not found`)
	s := &stubForward{handled: true, err: boom}
	out, exit, err := dispatchUnknown(context.Background(), "some-kind", []string{"channel", "read"}, s.fwd)
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the daemon's own %v", err, boom)
	}
	if out != "" {
		t.Fatalf("output = %q, want empty when the daemon refused", out)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 so main()'s own error path answers", exit)
	}
}

func TestDispatchUnknownWithoutDaemonIsAnUnknownCommand(t *testing.T) {
	s := &stubForward{handled: false}
	out, exit, err := dispatchUnknown(context.Background(), "some-kind", []string{"channel"}, s.fwd)
	if exit != 2 {
		t.Fatalf("exit = %d, want 2: the unchanged unknown-command outcome", exit)
	}
	if err == nil || err.Error() != `unknown command "some-kind"` {
		t.Fatalf("error = %v, want `unknown command \"some-kind\"`", err)
	}
	if out != "" {
		t.Fatalf("output = %q, want empty", out)
	}
}
