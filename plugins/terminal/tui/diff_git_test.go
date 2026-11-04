package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "-C", dir, "-c", "init.defaultBranch=master", "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	gitIn(t, dir, "config", "user.name", "Test Author")
	gitIn(t, dir, "config", "user.email", "author@example.com")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	writeRepoFile(t, dir, "a.txt", "one\ntwo\n")
	writeRepoFile(t, dir, "c.txt", "gone\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-m", "init")
	return dir
}

type scriptedRunner struct {
	prURL   string
	ghCalls [][]string
}

func (s *scriptedRunner) run(ctx context.Context, dir string, env []string, tool cliTool, args ...string) (string, error) {
	if tool == toolGh {
		s.ghCalls = append(s.ghCalls, args)
		return s.prURL, nil
	}
	if len(args) > 0 && args[0] == "push" {
		return "", nil
	}
	return execRunner(ctx, dir, env, tool, args...)
}

type fileExpectation struct {
	status  changeKind
	added   int
	deleted int
}

func TestLoadDiffListsEachFileWithItsStatusAndCounts(t *testing.T) {
	dir := initRepo(t)
	writeRepoFile(t, dir, "a.txt", "one\nTWO\nthree\n")
	writeRepoFile(t, dir, "b.txt", "new\n")
	gitIn(t, dir, "rm", "-q", "c.txt")

	report, err := newGitRepo(dir, nil).loadDiff(t.Context(), diffScopeTurn, "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]fileExpectation{
		"a.txt": {changeModified, 2, 1},
		"b.txt": {changeAdded, 1, 0},
		"c.txt": {changeDeleted, 0, 1},
	}
	if len(report.Files) != len(want) {
		t.Fatalf("files = %+v, want %d entries", report.Files, len(want))
	}
	for _, file := range report.Files {
		exp, ok := want[file.Path]
		if !ok {
			t.Fatalf("unexpected file %q", file.Path)
		}
		if file.Status != exp.status || file.Added != exp.added || file.Deleted != exp.deleted {
			t.Fatalf("%s = %v +%d -%d, want %v +%d -%d", file.Path, file.Status, file.Added, file.Deleted, exp.status, exp.added, exp.deleted)
		}
	}
}

func TestLoadDiffReportsBinaryFilesWithoutTextRows(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "img.bin"), []byte{0, 1, 2, 0}, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := newGitRepo(dir, nil).loadDiff(t.Context(), diffScopeTurn, "")
	if err != nil {
		t.Fatal(err)
	}
	file, ok := findDiffFile(report.Files, "img.bin")
	if !ok {
		t.Fatalf("img.bin missing from %+v", report.Files)
	}
	if !file.Binary || len(file.Rows) != 0 {
		t.Fatalf("img.bin = %+v, want binary without rows", file)
	}
}

func TestTurnScopeOnlyShowsChangesAfterTheTurnStarted(t *testing.T) {
	dir := initRepo(t)
	writeRepoFile(t, dir, "a.txt", "one\nTWO\n")
	repo := newGitRepo(dir, nil)
	turnStart, err := repo.snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "b.txt", "during the turn\n")

	report, err := repo.loadDiff(t.Context(), diffScopeTurn, turnStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Files) != 1 || report.Files[0].Path != "b.txt" {
		t.Fatalf("turn files = %+v, want only b.txt", report.Files)
	}
}

func TestSnapshotLeavesTheRealIndexAlone(t *testing.T) {
	dir := initRepo(t)
	writeRepoFile(t, dir, "b.txt", "untracked\n")

	if _, err := newGitRepo(dir, nil).snapshot(t.Context()); err != nil {
		t.Fatal(err)
	}

	status := gitIn(t, dir, "status", "--porcelain")
	if !strings.Contains(status, "?? b.txt") {
		t.Fatalf("status = %q, want b.txt still untracked", status)
	}
}

func TestBranchScopeShowsCommitsAheadOfBase(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeRepoFile(t, dir, "a.txt", "one\nTWO\n")
	gitIn(t, dir, "commit", "-q", "-am", "feat: change a")

	report, err := newGitRepo(dir, nil).loadDiff(t.Context(), diffScopeBranch, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Base != "master" || report.Ahead != 1 {
		t.Fatalf("base = %q ahead = %d, want master and 1", report.Base, report.Ahead)
	}
	if _, ok := findDiffFile(report.Files, "a.txt"); !ok {
		t.Fatalf("branch files = %+v, want a.txt", report.Files)
	}
}

func TestCommitStagesEverythingWithTheGivenMessage(t *testing.T) {
	dir := initRepo(t)
	writeRepoFile(t, dir, "b.txt", "new\n")

	sha, err := newGitRepo(dir, nil).commit(t.Context(), "feat: ajouter b")
	if err != nil {
		t.Fatal(err)
	}
	if sha == "" {
		t.Fatal("commit returned an empty sha")
	}
	if subject := strings.TrimSpace(gitIn(t, dir, "log", "-1", "--format=%s")); subject != "feat: ajouter b" {
		t.Fatalf("subject = %q", subject)
	}
	if status := strings.TrimSpace(gitIn(t, dir, "status", "--porcelain")); status != "" {
		t.Fatalf("worktree not clean after commit: %q", status)
	}
}

func TestOpenPRPushesAndReturnsTheCreatedNumber(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	runner := &scriptedRunner{prURL: "https://github.com/o/r/pull/42\n"}

	number, err := newGitRepo(dir, runner.run).openPR(t.Context(), "master", "feat: change", "body")
	if err != nil {
		t.Fatal(err)
	}
	if number != 42 {
		t.Fatalf("number = %d, want 42", number)
	}
	if len(runner.ghCalls) != 1 || !containsArgs(runner.ghCalls[0], "--base", "master") || !containsArgs(runner.ghCalls[0], "--title", "feat: change") {
		t.Fatalf("gh calls = %v", runner.ghCalls)
	}
}

func TestOpenPRRefusesTheBaseBranch(t *testing.T) {
	dir := initRepo(t)
	runner := &scriptedRunner{prURL: "https://github.com/o/r/pull/1\n"}

	_, err := newGitRepo(dir, runner.run).openPR(t.Context(), "master", "t", "b")
	if !errors.Is(err, errOnBase) {
		t.Fatalf("err = %v, want errOnBase", err)
	}
	if len(runner.ghCalls) != 0 {
		t.Fatalf("gh must not run from the base branch: %v", runner.ghCalls)
	}
}

func TestLoadDiffOutsideARepositoryFails(t *testing.T) {
	_, err := newGitRepo(t.TempDir(), nil).loadDiff(t.Context(), diffScopeTurn, "")
	if !errors.Is(err, errNotRepo) {
		t.Fatalf("err = %v, want errNotRepo", err)
	}
}

func TestDraftCommitMessageNamesFilesAndScope(t *testing.T) {
	files := []diffFile{
		{Path: "internal/x/a.go", Status: changeAdded, Added: 3},
		{Path: "internal/x/b.go", Status: changeModified, Added: 1, Deleted: 2},
	}

	message := draftCommitMessage(files)

	subject, body, _ := strings.Cut(message, "\n\n")
	if subject != "feat(x): modifier a.go, b.go" {
		t.Fatalf("subject = %q", subject)
	}
	if !strings.Contains(body, "- internal/x/b.go (+1 -2)") {
		t.Fatalf("body = %q", body)
	}
}

func TestDraftCommitMessageIsEmptyWithoutFiles(t *testing.T) {
	if message := draftCommitMessage(nil); message != "" {
		t.Fatalf("message = %q, want empty", message)
	}
}

func findDiffFile(files []diffFile, path string) (diffFile, bool) {
	for _, file := range files {
		if file.Path == path {
			return file, true
		}
	}
	return diffFile{}, false
}

func containsArgs(args []string, flag, value string) bool {
	for i := range len(args) - 1 {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestSnapshotSeesSameSizeEditsRightAfterTheCommit(t *testing.T) {
	for i := range 20 {
		dir := initRepo(t)
		writeRepoFile(t, dir, "a.txt", "one\nTWO\n")

		tree, err := newGitRepo(dir, nil).snapshot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if got := gitIn(t, dir, "show", tree+":a.txt"); got != "one\nTWO\n" {
			t.Fatalf("iteration %d: snapshot holds %q, want the edited content", i, got)
		}
	}
}
