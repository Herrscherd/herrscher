package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
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

// mainSwitches returns every case value of every switch in main().
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
