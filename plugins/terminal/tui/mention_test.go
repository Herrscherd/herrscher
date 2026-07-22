package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMentionComplete verifies that, given a worktree listing, completing an
// @-mention inserts @relative/path as plain text into the draft.
func TestMentionComplete(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The word under the cursor is "@sr"; the only match is the src/ directory.
	input := "foo @sr"
	start, prefix, ok := mentionWord(input, len(input))
	if !ok || prefix != "sr" || start != 4 {
		t.Fatalf("mentionWord = (%d,%q,%v), want (4,\"sr\",true)", start, prefix, ok)
	}
	matches := mentionMatches(dir, prefix)
	if len(matches) != 1 || matches[0] != "src/" {
		t.Fatalf("mentionMatches = %v, want [src/]", matches)
	}
	out, cur := completeMention(input, len(input), matches[0])
	if out != "foo @src/" {
		t.Fatalf("completeMention = %q, want %q", out, "foo @src/")
	}
	if cur != len(out) {
		t.Fatalf("cursor = %d, want %d", cur, len(out))
	}
}

// TestMentionWordNotAMention returns ok=false when the cursor word has no @.
func TestMentionWordNotAMention(t *testing.T) {
	if _, _, ok := mentionWord("plain text", 10); ok {
		t.Fatal("a non-@ word must not open a mention")
	}
}

// TestMentionMatchesDegradesOnBadDir returns nil (free-text fallback) when the
// worktree dir cannot be read.
func TestMentionMatchesDegradesOnBadDir(t *testing.T) {
	if got := mentionMatches(filepath.Join(t.TempDir(), "nope"), ""); got != nil {
		t.Fatalf("unreadable dir must degrade to nil, got %v", got)
	}
}
