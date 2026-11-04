package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	gitCallLimit    = 15 * time.Second
	prCallLimit     = 2 * time.Minute
	diffLoadLimit   = 45 * time.Second
	diffScopeTurn   = "turn"
	diffScopeBranch = "branch"
	diffPatchFlags  = "--no-renames"
	gitDiffPrefix   = "diff --git "
	diffNewPath     = " b/"
)

var (
	errNotRepo = errors.New("aucun dépôt git dans le dossier de cette session")
	errNoBase  = errors.New("aucune branche de base trouvée (origin/HEAD, master ou main)")
	errOnBase  = errors.New("cette branche est la branche de base : pas de PR depuis elle")
	gitEnv     = []string{"GIT_TERMINAL_PROMPT=0"}
	ghEnv      = []string{"GH_PROMPT_DISABLED=1"}
	pullNumber = regexp.MustCompile(`/pull/(\d+)`)
	hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

type commandRunner func(ctx context.Context, dir string, env []string, name string, args ...string) (string, error)

func execRunner(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("%s: no arguments", name)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return stdout.String(), fmt.Errorf("%s %s: %s", name, args[0], detail)
	}
	return stdout.String(), nil
}

type diffFile struct {
	Path    string
	Status  byte
	Added   int
	Deleted int
	Binary  bool
	Rows    []gitRow
}

type gitRow struct {
	Kind byte
	Old  int
	New  int
	Text string
}

type diffReport struct {
	Branch string
	Base   string
	Ahead  int
	Author string
	Files  []diffFile
}

type gitRepo struct {
	dir string
	run commandRunner
}

func newGitRepo(dir string, run commandRunner) gitRepo {
	if run == nil {
		run = execRunner
	}
	return gitRepo{dir: dir, run: run}
}

func (r gitRepo) call(parent context.Context, limit time.Duration, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	return r.run(ctx, r.dir, env, name, args...)
}

func (r gitRepo) git(ctx context.Context, args ...string) (string, error) {
	return r.call(ctx, gitCallLimit, gitEnv, "git", args...)
}

func (r gitRepo) snapshot(ctx context.Context) (string, error) {
	scratch, err := os.MkdirTemp("", "herrscher-diff-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	env := append(append([]string{}, gitEnv...), "GIT_INDEX_FILE="+filepath.Join(scratch, "index"))
	if _, err := r.headTree(ctx); err == nil {
		if _, err := r.call(ctx, gitCallLimit, env, "git", "read-tree", "HEAD"); err != nil {
			return "", err
		}
	}
	if _, err := r.call(ctx, gitCallLimit, env, "git", "add", "-A"); err != nil {
		return "", err
	}
	tree, err := r.call(ctx, gitCallLimit, env, "git", "write-tree")
	return strings.TrimSpace(tree), err
}

func (r gitRepo) baseBranch(ctx context.Context) string {
	if out, err := r.git(ctx, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name := strings.TrimPrefix(strings.TrimSpace(out), "origin/"); name != "" {
			return name
		}
	}
	for _, candidate := range []string{"master", "main"} {
		if _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func (r gitRepo) headTree(ctx context.Context) (string, error) {
	out, err := r.git(ctx, "rev-parse", "HEAD^{tree}")
	return strings.TrimSpace(out), err
}

func (r gitRepo) diffFrom(ctx context.Context, scope, turnStart string, report *diffReport) (string, error) {
	if scope == diffScopeBranch {
		if report.Base == "" {
			return "", errNoBase
		}
		mergeBase, err := r.git(ctx, "merge-base", "HEAD", report.Base)
		if err != nil {
			return "", err
		}
		ahead, _ := r.git(ctx, "rev-list", "--count", report.Base+"..HEAD")
		report.Ahead, _ = strconv.Atoi(strings.TrimSpace(ahead))
		return strings.TrimSpace(mergeBase), nil
	}
	if turnStart != "" {
		return turnStart, nil
	}
	return r.headTree(ctx)
}

func (r gitRepo) loadDiff(ctx context.Context, scope, turnStart string) (diffReport, error) {
	var report diffReport
	if _, err := r.git(ctx, "rev-parse", "--is-inside-work-tree"); err != nil {
		return report, errNotRepo
	}
	branch, _ := r.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	report.Branch = strings.TrimSpace(branch)
	author, _ := r.git(ctx, "config", "user.name")
	report.Author = strings.TrimSpace(author)
	report.Base = r.baseBranch(ctx)
	now, err := r.snapshot(ctx)
	if err != nil {
		return report, err
	}
	from, err := r.diffFrom(ctx, scope, turnStart, &report)
	if err != nil {
		return report, err
	}
	names, err := r.git(ctx, "diff", diffPatchFlags, "--name-status", from, now)
	if err != nil {
		return report, err
	}
	stats, err := r.git(ctx, "diff", diffPatchFlags, "--numstat", from, now)
	if err != nil {
		return report, err
	}
	patch, err := r.git(ctx, "diff", diffPatchFlags, "--no-color", "--no-ext-diff", "-U3", "--src-prefix=a/", "--dst-prefix=b/", from, now)
	if err != nil {
		return report, err
	}
	report.Files = parseDiff(names, stats, patch)
	return report, nil
}

func parseDiff(names, stats, patch string) []diffFile {
	files := parseNameStatus(names)
	applyNumstat(files, stats)
	applyPatch(files, patch)
	return files
}

func parseNameStatus(out string) []diffFile {
	var files []diffFile
	for _, line := range splitLines(out) {
		status, file, ok := strings.Cut(line, "\t")
		if !ok || status == "" {
			continue
		}
		files = append(files, diffFile{Status: status[0], Path: file})
	}
	return files
}

func applyNumstat(files []diffFile, out string) {
	index := indexByPath(files)
	for _, line := range splitLines(out) {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		i, ok := index[fields[2]]
		if !ok {
			continue
		}
		if fields[0] == "-" {
			files[i].Binary = true
			continue
		}
		files[i].Added, _ = strconv.Atoi(fields[0])
		files[i].Deleted, _ = strconv.Atoi(fields[1])
	}
}

func applyPatch(files []diffFile, patch string) {
	index := indexByPath(files)
	current := -1
	inHunk := false
	oldNo, newNo := 0, 0
	for _, line := range strings.Split(patch, "\n") {
		if rest, ok := strings.CutPrefix(line, gitDiffPrefix); ok {
			current, inHunk = -1, false
			if at := strings.LastIndex(rest, diffNewPath); at >= 0 {
				if i, found := index[rest[at+len(diffNewPath):]]; found {
					current = i
				}
			}
			continue
		}
		if current < 0 {
			continue
		}
		if match := hunkHeader.FindStringSubmatch(line); match != nil {
			inHunk = true
			oldNo, _ = strconv.Atoi(match[1])
			newNo, _ = strconv.Atoi(match[2])
			files[current].Rows = append(files[current].Rows, gitRow{Kind: '@', Text: line})
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		text := line[1:]
		switch line[0] {
		case '+':
			files[current].Rows = append(files[current].Rows, gitRow{Kind: '+', New: newNo, Text: text})
			newNo++
		case '-':
			files[current].Rows = append(files[current].Rows, gitRow{Kind: '-', Old: oldNo, Text: text})
			oldNo++
		case ' ':
			files[current].Rows = append(files[current].Rows, gitRow{Kind: ' ', Old: oldNo, New: newNo, Text: text})
			oldNo++
			newNo++
		}
	}
}

func indexByPath(files []diffFile) map[string]int {
	index := make(map[string]int, len(files))
	for i, file := range files {
		index[file.Path] = i
	}
	return index
}

func splitLines(out string) []string {
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

func (r gitRepo) commit(ctx context.Context, message string) (string, error) {
	if _, err := r.git(ctx, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := r.git(ctx, "commit", "-m", message); err != nil {
		return "", err
	}
	sha, err := r.git(ctx, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(sha), err
}

func (r gitRepo) openPR(ctx context.Context, base, title, body string) (int, error) {
	branch, err := r.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(branch) == base {
		return 0, errOnBase
	}
	if _, err := r.call(ctx, prCallLimit, gitEnv, "git", "push", "-u", "origin", "HEAD"); err != nil {
		return 0, err
	}
	out, err := r.call(ctx, prCallLimit, ghEnv, "gh", "pr", "create", "--base", base, "--title", title, "--body", body)
	if err != nil {
		return 0, err
	}
	match := pullNumber.FindStringSubmatch(out)
	if match == nil {
		return 0, fmt.Errorf("gh pr create : aucun numéro de PR dans %q", strings.TrimSpace(out))
	}
	return strconv.Atoi(match[1])
}

func draftCommitMessage(files []diffFile) string {
	if len(files) == 0 {
		return ""
	}
	kind, verb := "chore", "modifier"
	if hasAdded(files) {
		kind = "feat"
	}
	if allAdded(files) {
		verb = "ajouter"
	}
	subject := fmt.Sprintf("%s: %s %s", kind, verb, summarizeNames(files))
	if scope := commonDirName(files); scope != "" {
		subject = fmt.Sprintf("%s(%s): %s %s", kind, scope, verb, summarizeNames(files))
	}
	lines := make([]string, 0, len(files))
	for _, file := range files {
		lines = append(lines, fmt.Sprintf("- %s (+%d -%d)", file.Path, file.Added, file.Deleted))
	}
	return subject + "\n\n" + strings.Join(lines, "\n")
}

func hasAdded(files []diffFile) bool {
	for _, file := range files {
		if file.Status == 'A' {
			return true
		}
	}
	return false
}

func allAdded(files []diffFile) bool {
	for _, file := range files {
		if file.Status != 'A' {
			return false
		}
	}
	return true
}

const draftNameLimit = 3

func summarizeNames(files []diffFile) string {
	names := make([]string, 0, draftNameLimit)
	for _, file := range files {
		if len(names) == draftNameLimit {
			break
		}
		names = append(names, path.Base(file.Path))
	}
	summary := strings.Join(names, ", ")
	if rest := len(files) - len(names); rest > 0 {
		summary += fmt.Sprintf(" et %d autres", rest)
	}
	return summary
}

func commonDirName(files []diffFile) string {
	dir := path.Dir(files[0].Path)
	for _, file := range files[1:] {
		if path.Dir(file.Path) != dir {
			return ""
		}
	}
	if dir == "." {
		return ""
	}
	return path.Base(dir)
}
