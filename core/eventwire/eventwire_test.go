package eventwire

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEventValuesAreFrozen(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"EventChunk", EventChunk, "chunk"},
		{"EventThinking", EventThinking, "thinking"},
		{"EventStatus", EventStatus, "status"},
		{"EventTodos", EventTodos, "todos"},
		{"EventSubagent", EventSubagent, "subagent"},
		{"EventReset", EventReset, "reset"},
		{"EventReply", EventReply, "reply"},
		{"EventAbandoned", EventAbandoned, "abandoned"},
		{"EventClosed", EventClosed, "closed"},
		{"EventNotice", EventNotice, "notice"},
		{"EventBackendNotice", EventBackendNotice, "backend_notice"},
		{"EventInput", EventInput, "input"},
		{"EventHuman", EventHuman, "human"},
		{"EventPaused", EventPaused, "paused"},
		{"EventInterrupt", EventInterrupt, "interrupt"},
		{"EventPick", EventPick, "pick"},
		{"EventLanguage", EventLanguage, "language"},
		{"EventMenu", EventMenu, "menu"},
		{"EventApproval", EventApproval, "approval"},
		{"EventApprovalRisk", EventApprovalRisk, "approval_risk"},
		{"EventApprovalSubject", EventApprovalSubject, "approval_subject"},
		{"EventCompactNow", EventCompactNow, "compact_now"},
		{"EventCompactionStarted", EventCompactionStarted, "compaction_started"},
		{"EventCompactionDone", EventCompactionDone, "compaction_done"},
		{"EventCompactionFailed", EventCompactionFailed, "compaction_failed"},
		{"CompactReasonBridgeGone", CompactReasonBridgeGone, "bridge_gone"},
		{"CompactReasonNoCompactorFactory", CompactReasonNoCompactorFactory, "no_compactor_factory"},
		{"CompactReasonNilCompactor", CompactReasonNilCompactor, "nil_compactor"},
		{"CompactReasonNoCompactorPlugin", CompactReasonNoCompactorPlugin, "no_compactor_plugin"},
		{"CompactReasonNoHistoryPort", CompactReasonNoHistoryPort, "no_history_port"},
		{"CompactReasonHistoryTimeout", CompactReasonHistoryTimeout, "history_timeout"},
		{"CompactReasonHistoryTooLarge", CompactReasonHistoryTooLarge, "history_too_large"},
		{"CompactReasonTooManyEdits", CompactReasonTooManyEdits, "too_many_edits"},
		{"CompactReasonUnknown", CompactReasonUnknown, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestEventTableCoversEveryConstant(t *testing.T) {
	declared := packageConstants(t)
	listed := tableConstants(t, "eventwire_test.go")
	for name := range declared {
		if !listed[name] {
			t.Errorf("constant %s has no row in TestEventValuesAreFrozen", name)
		}
	}
	for name := range listed {
		if !declared[name] {
			t.Errorf("row names %s, which is not a constant of the package", name)
		}
	}
}

func parseSource(t *testing.T, path string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}

func packageConstants(t *testing.T) map[string]bool {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list package files: %v", err)
	}
	names := make(map[string]bool)
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		collectConstants(t, path, names)
	}
	if len(names) == 0 {
		t.Fatal("no constant found in the package sources")
	}
	return names
}

func collectConstants(t *testing.T, path string, names map[string]bool) {
	t.Helper()
	for _, decl := range parseSource(t, path).Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, ident := range value.Names {
				names[ident.Name] = true
			}
		}
	}
}

func tableConstants(t *testing.T, path string) map[string]bool {
	t.Helper()
	names := make(map[string]bool)
	ast.Inspect(parseSource(t, path), func(node ast.Node) bool {
		row, ok := node.(*ast.CompositeLit)
		if !ok || len(row.Elts) != 3 {
			return true
		}
		label, okLabel := row.Elts[0].(*ast.BasicLit)
		ident, okIdent := row.Elts[1].(*ast.Ident)
		if !okLabel || !okIdent {
			return true
		}
		quoted, err := strconv.Unquote(label.Value)
		if err != nil {
			t.Fatalf("row label %s: %v", label.Value, err)
		}
		if quoted != ident.Name {
			t.Errorf("row labelled %q tests constant %s", quoted, ident.Name)
		}
		names[ident.Name] = true
		return true
	})
	return names
}
