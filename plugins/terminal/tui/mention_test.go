package tui

import (
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestMentionComplete(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

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

func TestWorktreeDirPrefersSessionDir(t *testing.T) {
	dir := t.TempDir()
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "a", ChannelID: "a", Dir: dir}}}
	m := newModel(f)
	m.ensureTab("a")
	if got := m.worktreeDir(); got != dir {
		t.Fatalf("worktreeDir = %q, want the session dir %q", got, dir)
	}
}

func TestWorktreeDirFallsBackToCwd(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "a", ChannelID: "a"}}}
	m := newModel(f)
	m.ensureTab("a")
	wd, _ := os.Getwd()
	if got := m.worktreeDir(); got != wd {
		t.Fatalf("worktreeDir = %q, want cwd %q", got, wd)
	}
}

func TestMentionWordNotAMention(t *testing.T) {
	if _, _, ok := mentionWord("plain text", 10); ok {
		t.Fatal("a non-@ word must not open a mention")
	}
}

func TestMentionMatchesDegradesOnBadDir(t *testing.T) {
	if got := mentionMatches(filepath.Join(t.TempDir(), "nope"), ""); got != nil {
		t.Fatalf("unreadable dir must degrade to nil, got %v", got)
	}
}
