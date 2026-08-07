package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

// The verbs main() answers in-process. Contributed verbs reach the CLI by NOT
// being here — they fall through to the daemon — so the day one of these starts
// forwarding instead, every existing user's `session list` quietly changes
// meaning. This list is that boundary, asserted rather than assumed.
var expectedLocalVerbs = []string{
	"init", "plugin", "update", "install",
	"bridge", "serve", "session", "agent", "memory", "models", "service", "plugin-host",
	"-h", "--help", "help",
}

// mainSwitches returns every case value of every switch in main(), and whether
// main()'s default arm hands the argv to the daemon forwarder.
func mainSwitches(t *testing.T) (cases []string, defaultForwards bool) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if d, ok := d.(*ast.FuncDecl); ok && d.Name.Name == "main" && d.Recv == nil {
			fn = d
		}
	}
	if fn == nil {
		t.Fatal("no main() in main.go")
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		cl, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		if cl.List == nil {
			ast.Inspect(cl, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "forwardUnknownVerb" {
					defaultForwards = true
				}
				return true
			})
			return true
		}
		for _, e := range cl.List {
			if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				cases = append(cases, lit.Value[1:len(lit.Value)-1])
			}
		}
		return true
	})
	return cases, defaultForwards
}

func TestKnownVerbsKeepTheirLocalPath(t *testing.T) {
	cases, _ := mainSwitches(t)
	seen := map[string]bool{}
	for _, c := range cases {
		seen[c] = true
	}
	for _, v := range expectedLocalVerbs {
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

func TestForwardUnknownVerbWithoutDaemon(t *testing.T) {
	// No daemon anywhere: the socket path is rooted in this test's temp dir, and
	// the state file it would name the instance from does not exist either.
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

// stubForward records the argv it was offered and replays a canned answer, so
// the dispatch decision can be tested without a daemon or a re-exec.
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
	// The verb is part of what the daemon must read: it is the namespace the
	// contributed command lives under, so relaying only the tail addresses the
	// registry with a command nobody registered.
	s := &stubForward{out: "two messages", handled: true}
	args := []string{"channel", "read", "--limit", "5"}
	out, err, exit := dispatchUnknown(context.Background(), "some-kind", args, s.fwd)

	want := []string{"some-kind", "channel", "read", "--limit", "5"}
	if !reflect.DeepEqual(s.argv, want) {
		t.Fatalf("forwarded argv = %v, want %v", s.argv, want)
	}
	if out != "two messages" || err != nil || exit != 0 {
		t.Fatalf("dispatchUnknown = (%q, %v, %d), want (\"two messages\", nil, 0)", out, err, exit)
	}
}

func TestDispatchUnknownKeepsTheDaemonsFailure(t *testing.T) {
	// Swallowing derr would exit 0 on a failed command: a silent success for any
	// agent or script reading $?.
	boom := errors.New(`plugin: channel "nope" not found`)
	s := &stubForward{handled: true, err: boom}
	out, err, exit := dispatchUnknown(context.Background(), "some-kind", []string{"channel", "read"}, s.fwd)
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the daemon's own %v", err, boom)
	}
	if out != "" {
		t.Fatalf("output = %q, want empty when the daemon refused", out)
	}
	// exit 0 hands the failure back to main(), which turns any non-nil err into
	// its usual exit 1 — the daemon does not get to pick the process's code.
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 so main()'s own error path answers", exit)
	}
}

func TestDispatchUnknownWithoutDaemonIsAnUnknownCommand(t *testing.T) {
	s := &stubForward{handled: false}
	out, err, exit := dispatchUnknown(context.Background(), "some-kind", []string{"channel"}, s.fwd)
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
