package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeReferenceFileLoadsWithoutWarnings(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-reference-keybindings.json"))
	if err != nil {
		t.Fatal(err)
	}
	load := loadForTest(t, "", string(raw), keysSection{})
	if len(load.problems) > 0 {
		t.Fatalf("the Claude reference file must load without warnings: %s", joinProblems(load.problems))
	}
}

func TestEveryClaudeReferenceActionIsAccepted(t *testing.T) {
	for _, action := range claudeReferenceActions() {
		file := `{"bindings":[{"context":"Global","bindings":{"ctrl+x z":"` + string(action) + `"}}]}`
		if load := loadForTest(t, "", file, keysSection{}); len(load.problems) > 0 {
			t.Errorf("reference action %s is rejected: %s", action, joinProblems(load.problems))
		}
	}
}
