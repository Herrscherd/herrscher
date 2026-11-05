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
	gitCallLimit   = 15 * time.Second
	prCallLimit    = 2 * time.Minute
	diffLoadLimit  = 45 * time.Second
	diffPatchFlags = "--no-renames"
	gitDiffPrefix  = "diff --git "
	diffNewPath    = " b/"
	numstatFields  = 3
	numstatBinary  = "-"
	gitExitMissing = 1
	draftNameLimit = 3
	noFile         = -1
)

var (
	errNotRepo       = errors.New("no git repository in this session's folder")
	errNoBase        = errors.New("no base branch found (origin/HEAD, master or main)")
	errOnBase        = errors.New("this branch is the base branch: no PR from it")
	errNoArguments   = errors.New("command without argument")
	errUnknownTool   = errors.New("unknown tool")
	errUnknownScope  = errors.New("unknown diff scope")
	errUnknownChange = errors.New("unknown git file status")
	errMalformedStat = errors.New("unexpected git output")
	errNoPullNumber  = errors.New("no PR number in gh output")
	pullNumber       = regexp.MustCompile(`/pull/(\d+)`)
	hunkHeader       = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

type cliTool int

const (
	toolGit cliTool = iota + 1
	toolGh
)

func (tool cliTool) String() string {
	switch tool {
	case toolGit:
		return "git"
	case toolGh:
		return "gh"
	}
	return ""
}

type commandError struct {
	tool     cliTool
	verb     string
	detail   string
	exitCode int
	cause    error
}

func (e *commandError) Error() string {
	return e.tool.String() + " " + e.verb + ": " + e.detail
}

func (e *commandError) Unwrap() error {
	return e.cause
}

type commandRunner func(ctx context.Context, dir string, env []string, tool cliTool, args ...string) (string, error)

func gitEnvironment() []string {
	return []string{"GIT_TERMINAL_PROMPT=0"}
}

func ghEnvironment() []string {
	return []string{"GH_PROMPT_DISABLED=1"}
}

func execRunner(ctx context.Context, dir string, env []string, tool cliTool, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errNoArguments
	}
	cmd, err := commandFor(ctx, tool, args)
	if err != nil {
		return "", err
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err = cmd.Run(); err != nil {
		return stdout.String(), commandFailure(tool, args[0], stderr.String(), err)
	}
	return stdout.String(), nil
}

func commandFor(ctx context.Context, tool cliTool, args []string) (*exec.Cmd, error) {
	switch tool {
	case toolGit:
		return exec.CommandContext(ctx, "git", args...), nil
	case toolGh:
		return exec.CommandContext(ctx, "gh", args...), nil
	default:
		return nil, errUnknownTool
	}
}

func commandFailure(tool cliTool, verb, stderr string, cause error) error {
	detail := strings.TrimSpace(stderr)
	if detail == "" {
		detail = cause.Error()
	}
	failure := &commandError{tool: tool, verb: verb, detail: detail, exitCode: -1, cause: cause}
	if exitErr, ok := errors.AsType[*exec.ExitError](cause); ok {
		failure.exitCode = exitErr.ExitCode()
	}
	return failure
}

type diffScope int

const (
	diffScopeTurn diffScope = iota + 1
	diffScopeBranch
)

type changeKind int

const (
	changeAdded changeKind = iota + 1
	changeModified
	changeDeleted
	changeTypeChanged
	changeUnmerged
)

func parseChangeKind(letter string) (changeKind, error) {
	switch letter {
	case "A":
		return changeAdded, nil
	case "M":
		return changeModified, nil
	case "D":
		return changeDeleted, nil
	case "T":
		return changeTypeChanged, nil
	case "U":
		return changeUnmerged, nil
	}
	return 0, fmt.Errorf("%w : %q", errUnknownChange, letter)
}

func (kind changeKind) String() string {
	switch kind {
	case changeAdded:
		return "A"
	case changeModified:
		return "M"
	case changeDeleted:
		return "D"
	case changeTypeChanged:
		return "T"
	case changeUnmerged:
		return "U"
	}
	return "?"
}

type rowKind int

const (
	rowHunk rowKind = iota + 1
	rowAdded
	rowRemoved
	rowContext
)

func rowKindOf(marker byte) (rowKind, bool) {
	switch marker {
	case '+':
		return rowAdded, true
	case '-':
		return rowRemoved, true
	case ' ':
		return rowContext, true
	}
	return 0, false
}

func (kind rowKind) String() string {
	switch kind {
	case rowHunk:
		return "@"
	case rowAdded:
		return "+"
	case rowRemoved:
		return "-"
	case rowContext:
		return " "
	}
	return ""
}

type diffFile struct {
	Path    string
	Status  changeKind
	Added   int
	Deleted int
	Binary  bool
	Rows    []gitRow
}

type gitRow struct {
	Kind rowKind
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

func (r gitRepo) call(parent context.Context, limit time.Duration, env []string, tool cliTool, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	return r.run(ctx, r.dir, env, tool, args...)
}

func (r gitRepo) git(ctx context.Context, args ...string) (string, error) {
	return r.call(ctx, gitCallLimit, gitEnvironment(), toolGit, args...)
}

func (r gitRepo) optionalValue(ctx context.Context, args ...string) (string, error) {
	out, err := r.git(ctx, args...)
	if failure, ok := errors.AsType[*commandError](err); ok && failure.exitCode == gitExitMissing {
		return "", nil
	}
	return strings.TrimSpace(out), err
}

func (r gitRepo) snapshot(ctx context.Context) (string, error) {
	scratch, err := os.MkdirTemp("", "herrscher-diff-index-")
	if err != nil {
		return "", err
	}
	tree, err := r.writeSnapshotTree(ctx, scratch)
	return tree, errors.Join(err, os.RemoveAll(scratch))
}

func (r gitRepo) writeSnapshotTree(ctx context.Context, scratch string) (string, error) {
	env := append(gitEnvironment(), "GIT_INDEX_FILE="+filepath.Join(scratch, "index"))
	if r.hasHead(ctx) {
		if _, err := r.call(ctx, gitCallLimit, env, toolGit, "read-tree", "HEAD"); err != nil {
			return "", err
		}
	}
	if _, err := r.call(ctx, gitCallLimit, env, toolGit, "add", "-A"); err != nil {
		return "", err
	}
	tree, err := r.call(ctx, gitCallLimit, env, toolGit, "write-tree")
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

func (r gitRepo) hasHead(ctx context.Context) bool {
	_, err := r.headTree(ctx)
	return err == nil
}

func (r gitRepo) diffFrom(ctx context.Context, scope diffScope, turnStart string, report *diffReport) (string, error) {
	switch scope {
	case diffScopeBranch:
		return r.branchOrigin(ctx, report)
	case diffScopeTurn:
		if turnStart != "" {
			return turnStart, nil
		}
		return r.headTree(ctx)
	}
	return "", errUnknownScope
}

func (r gitRepo) branchOrigin(ctx context.Context, report *diffReport) (string, error) {
	if report.Base == "" {
		return "", errNoBase
	}
	mergeBase, err := r.git(ctx, "merge-base", "HEAD", report.Base)
	if err != nil {
		return "", err
	}
	ahead, err := r.git(ctx, "rev-list", "--count", report.Base+"..HEAD")
	if err != nil {
		return "", err
	}
	if report.Ahead, err = strconv.Atoi(strings.TrimSpace(ahead)); err != nil {
		return "", err
	}
	return strings.TrimSpace(mergeBase), nil
}

func (r gitRepo) loadDiff(ctx context.Context, scope diffScope, turnStart string) (diffReport, error) {
	var report diffReport
	if _, err := r.git(ctx, "rev-parse", "--is-inside-work-tree"); err != nil {
		return report, errNotRepo
	}
	var err error
	if report.Branch, err = r.optionalValue(ctx, "symbolic-ref", "--quiet", "--short", "HEAD"); err != nil {
		return report, err
	}
	if report.Author, err = r.optionalValue(ctx, "config", "--get", "user.name"); err != nil {
		return report, err
	}
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
	if report.Files, err = parseDiff(names, stats, patch); err != nil {
		return report, err
	}
	return report, nil
}

func parseDiff(names, stats, patch string) ([]diffFile, error) {
	files, err := parseNameStatus(names)
	if err != nil {
		return nil, err
	}
	if err := applyNumstat(files, stats); err != nil {
		return nil, err
	}
	if err := applyPatch(files, patch); err != nil {
		return nil, err
	}
	return files, nil
}

func parseNameStatus(out string) ([]diffFile, error) {
	var files []diffFile
	for _, line := range splitLines(out) {
		letter, file, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("%w : %q", errMalformedStat, line)
		}
		kind, err := parseChangeKind(letter)
		if err != nil {
			return nil, err
		}
		files = append(files, diffFile{Status: kind, Path: file})
	}
	return files, nil
}

func applyNumstat(files []diffFile, out string) error {
	index := indexByPath(files)
	for _, line := range splitLines(out) {
		fields := strings.SplitN(line, "\t", numstatFields)
		if len(fields) != numstatFields {
			return fmt.Errorf("%w : %q", errMalformedStat, line)
		}
		position, known := index[fields[2]]
		if !known {
			continue
		}
		if fields[0] == numstatBinary {
			files[position].Binary = true
			continue
		}
		added, err := strconv.Atoi(fields[0])
		if err != nil {
			return err
		}
		deleted, err := strconv.Atoi(fields[1])
		if err != nil {
			return err
		}
		files[position].Added, files[position].Deleted = added, deleted
	}
	return nil
}

type patchReader struct {
	files   []diffFile
	index   map[string]int
	current int
	inHunk  bool
	oldLine int
	newLine int
}

func applyPatch(files []diffFile, patch string) error {
	reader := &patchReader{files: files, index: indexByPath(files), current: noFile}
	for _, line := range strings.Split(patch, "\n") {
		if err := reader.consume(line); err != nil {
			return err
		}
	}
	return nil
}

func (reader *patchReader) consume(line string) error {
	if header, ok := strings.CutPrefix(line, gitDiffPrefix); ok {
		reader.openFile(header)
		return nil
	}
	if reader.current == noFile {
		return nil
	}
	if match := hunkHeader.FindStringSubmatch(line); match != nil {
		return reader.openHunk(line, match)
	}
	if !reader.inHunk || line == "" {
		return nil
	}
	if kind, ok := rowKindOf(line[0]); ok {
		reader.addRow(kind, line[1:])
	}
	return nil
}

func (reader *patchReader) openFile(header string) {
	reader.current, reader.inHunk = noFile, false
	marker := strings.LastIndex(header, diffNewPath)
	if marker < 0 {
		return
	}
	if position, found := reader.index[header[marker+len(diffNewPath):]]; found {
		reader.current = position
	}
}

func (reader *patchReader) openHunk(line string, match []string) error {
	oldStart, err := strconv.Atoi(match[1])
	if err != nil {
		return err
	}
	newStart, err := strconv.Atoi(match[2])
	if err != nil {
		return err
	}
	reader.inHunk, reader.oldLine, reader.newLine = true, oldStart, newStart
	reader.appendRow(gitRow{Kind: rowHunk, Text: line})
	return nil
}

func (reader *patchReader) addRow(kind rowKind, text string) {
	row := gitRow{Kind: kind, Text: text}
	switch kind {
	case rowAdded:
		row.New = reader.newLine
		reader.newLine++
	case rowRemoved:
		row.Old = reader.oldLine
		reader.oldLine++
	case rowContext:
		row.Old, row.New = reader.oldLine, reader.newLine
		reader.oldLine++
		reader.newLine++
	case rowHunk:
	}
	reader.appendRow(row)
}

func (reader *patchReader) appendRow(row gitRow) {
	file := &reader.files[reader.current]
	file.Rows = append(file.Rows, row)
}

func indexByPath(files []diffFile) map[string]int {
	index := make(map[string]int, len(files))
	for position, file := range files {
		index[file.Path] = position
	}
	return index
}

func splitLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
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
	if _, err = r.call(ctx, prCallLimit, gitEnvironment(), toolGit, "push", "-u", "origin", "HEAD"); err != nil {
		return 0, err
	}
	out, err := r.call(ctx, prCallLimit, ghEnvironment(), toolGh, "pr", "create", "--base", base, "--title", title, "--body", body)
	if err != nil {
		return 0, err
	}
	match := pullNumber.FindStringSubmatch(out)
	if match == nil {
		return 0, fmt.Errorf("%w : %q", errNoPullNumber, strings.TrimSpace(out))
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
	names := summarizeNames(files)
	subject := fmt.Sprintf("%s: %s %s", kind, verb, names)
	if scope := commonDirName(files); scope != "" {
		subject = fmt.Sprintf("%s(%s): %s %s", kind, scope, verb, names)
	}
	lines := make([]string, 0, len(files))
	for _, file := range files {
		lines = append(lines, fmt.Sprintf("- %s (+%d -%d)", file.Path, file.Added, file.Deleted))
	}
	return subject + "\n\n" + strings.Join(lines, "\n")
}

func hasAdded(files []diffFile) bool {
	for _, file := range files {
		if file.Status == changeAdded {
			return true
		}
	}
	return false
}

func allAdded(files []diffFile) bool {
	for _, file := range files {
		if file.Status != changeAdded {
			return false
		}
	}
	return true
}

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
	if len(files) == 0 {
		return ""
	}
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
