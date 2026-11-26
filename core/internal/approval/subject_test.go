package approval

import "testing"

func TestSubjectOf(t *testing.T) {
	cases := []struct {
		tool  string
		input map[string]any
		want  string
	}{
		{"Bash", map[string]any{"command": "git push"}, "git push"},
		{"Edit", map[string]any{"file_path": "/repo/main.go"}, "/repo/main.go"},
		{"Write", map[string]any{"file_path": "/repo/new.go"}, "/repo/new.go"},
		{"Read", map[string]any{"file_path": "/etc/passwd"}, "/etc/passwd"},
		{"NotebookEdit", map[string]any{"notebook_path": "/repo/x.ipynb"}, "/repo/x.ipynb"},
		{"WebFetch", map[string]any{"url": "https://example.com"}, "https://example.com"},
		{"WebSearch", map[string]any{"query": "herrscher"}, "herrscher"},
		{"mcp__neublox__place", map[string]any{"anything": 1}, ""},
		{"Bash", map[string]any{"command": 42}, ""},
		{"Bash", nil, ""},
	}
	for _, c := range cases {
		if got := SubjectOf(c.tool, c.input); got != c.want {
			t.Fatalf("SubjectOf(%q, %v): got %q, want %q", c.tool, c.input, got, c.want)
		}
	}
}

func TestGrokToolsMeetTheModesUnderTheirClaudeNames(t *testing.T) {
	cases := []struct {
		grok string
		mode Mode
		want Decision
	}{
		{"run_terminal_command", ModeAsk, Ask},
		{"search_replace", ModeAsk, Ask},
		{"hashline_edit", ModeAsk, Ask},
		{"write", ModeAsk, Ask},
		{"read_file", ModePlan, Allow},
		{"hashline_read", ModePlan, Allow},
		{"grep", ModePlan, Allow},
		{"hashline_grep", ModePlan, Allow},
		{"list_dir", ModePlan, Allow},
		{"todo_write", ModePlan, Allow},
		{"web_fetch", ModePlan, Allow},
		{"web_search", ModePlan, Allow},
		{"spawn_subagent", ModePlan, Deny},
		{"image_gen", ModePlan, Deny},
	}
	for _, c := range cases {
		if got := Apply(c.mode, CanonicalTool(c.grok), Allow, false); got != c.want {
			t.Errorf("%s in %s: got %q, want %q", c.grok, c.mode, got, c.want)
		}
	}
	if got := SubjectOf(CanonicalTool("read_file"), map[string]any{"target_file": "/repo/a.go"}); got != "/repo/a.go" {
		t.Fatalf("read_file subject = %q, want its target_file", got)
	}
}
