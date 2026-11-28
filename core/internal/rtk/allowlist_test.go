package rtk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyAllowsTheListedCommands(t *testing.T) {
	for _, command := range []string{
		"git status",
		"git status --porcelain -b",
		"git log --oneline -3",
		"git diff --no-patch --stat",
		"git show HEAD",
		"git branch",
		"git branch -a",
		"git branch -vv",
		"ls -la",
		"tree -I node_modules",
		"cat go.mod",
		"head -3 go.mod",
		"tail -n 5 go.mod",
		"wc -l go.mod",
		"grep -r foo .",
		"rg foo",
		"find . -name x",
		"gh pr view 1 --json title",
		"gh pr list",
		"gh pr diff 1",
		"gh pr checks 1",
		"gh issue view 1",
		"gh issue list",
		"gh run view 1",
		"gh run list",
	} {
		if !readOnly(command) {
			t.Errorf("readOnly(%q) = false, want true", command)
		}
	}
}

func TestReadOnlyRefusesShellSyntax(t *testing.T) {
	for _, command := range []string{
		"git status && ls",
		"git status || ls",
		"git status; ls",
		"git status | cat",
		"git status & ls",
		"ls > out",
		"ls < in",
		"ls $(whoami)",
		"ls `whoami`",
		"ls $HOME",
		"ls (x)",
		"ls \"a\"",
		"ls 'a'",
		"ls *.go",
		"ls ~",
		"ls a\\ b",
		"git status\nls",
		"git status\rls",
		"ls \\\n-la",
	} {
		if readOnly(command) {
			t.Errorf("readOnly(%q) = true, want false", command)
		}
	}
}

func TestReadOnlyRefusesWritesAndUnlistedPrograms(t *testing.T) {
	for _, command := range []string{
		"",
		"   ",
		"rm -rf x",
		"mv a b",
		"npm test",
		"cargo test",
		"make",
		"curl -s https://example.com",
		"wget https://example.com",
		"git push origin main",
		"git commit -m x",
		"git checkout x",
		"git reset --hard",
		"git -C /x status",
		"git branch newname",
		"gh api repos/x",
		"gh pr merge 1",
		"gh pr close 1",
		"gh issue create",
		"gh run rerun 1",
	} {
		if readOnly(command) {
			t.Errorf("readOnly(%q) = true, want false", command)
		}
	}
}

func TestReadOnlyRefusesEachWritingFlag(t *testing.T) {
	for _, command := range []string{
		"git branch -d foo",
		"git branch -D foo",
		"git branch -m a b",
		"git branch -M a b",
		"git branch --delete foo",
		"git branch --move a b",
		"git branch --force foo",
		"git branch -f foo",
		"git branch -c a b",
		"git branch -a -d foo",
		"git diff --output=x",
		"git diff --output x",
		"git log --output=x",
		"git show --output=x",
		"git status --output=x",
		"tree -o x",
		"tree -R",
		"tree -aR",
		"tree -oa",
		"find . -delete",
		"find . -exec rm +",
		"find . -execdir rm +",
		"find . -ok rm +",
		"find . -okdir rm +",
		"find . -fprint x",
		"find . -fprint0 x",
		"find . -fprintf x %p",
		"find . -fls x",
		"rg --pre cat foo",
		"rg --pre-glob x foo",
		"gh pr view 1 --web",
		"gh pr view 1 -w",
		"gh pr checks 1 --watch",
	} {
		if readOnly(command) {
			t.Errorf("readOnly(%q) = true, want false", command)
		}
	}
}

func TestRewriteNeverRunsRTKForAWrite(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "called")
	path := fakeRTK(t, `: > "`+marker+`"; printf 'rtk %s' "$2"`)
	if got, ok := Rewrite(context.Background(), path, "rm -rf x"); ok {
		t.Fatalf("a write rewrote to %q", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("rtk was run for a write")
	}
}

func TestRewriteGivesUpWhenRTKIsSlow(t *testing.T) {
	path := fakeRTK(t, `exec sleep 30`)
	start := time.Now()
	if got, ok := Rewrite(context.Background(), path, "git status"); ok {
		t.Fatalf("a slow rtk rewrote to %q", got)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("a slow rtk held the hook for %s", elapsed)
	}
}

func TestRewriteRefusesAnOutputOverTheCap(t *testing.T) {
	command := "ls " + strings.Repeat("a", 5000)
	path := fakeRTK(t, `printf 'rtk %s' "$2"`)
	if got, ok := Rewrite(context.Background(), path, command); ok {
		t.Fatalf("an output over the cap rewrote to a %d byte command", len(got))
	}
}
