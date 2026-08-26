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
