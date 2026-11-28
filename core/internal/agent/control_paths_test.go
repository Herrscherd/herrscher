package agent

import (
	"slices"
	"testing"
)

func TestControlPathsCoverGitAndEveryMaterializedFile(t *testing.T) {
	got := ControlPaths()
	for _, want := range []string{".git", "AGENTS.md", ".codex", ".claude", ".mcp.json", ".cursor", ".herrscher", ".grok"} {
		if !slices.Contains(got, want) {
			t.Errorf("ControlPaths() = %v, lacks %q", got, want)
		}
	}
}
