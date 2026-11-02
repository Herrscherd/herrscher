package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/redact"
)

const (
	subWorktree = "worktree"
)

const sessionsDir = ".herrscher-sessions"

var (
	errFlagBase     = errors.New("refusing a base ref that looks like a flag")
	errUncommitted  = errors.New("uncommitted changes")
	errNotAWorktree = errors.New("already exists and is not a git worktree")
	errOtherBranch  = errors.New("worktree is on another branch")
)

// Worktreer manages git worktrees. It is repo-stateless: the repo root is passed
// to each method, so one Worktreer serves every project in the workspace.
// With a non-empty instanceID, worktrees live under
// <repo>/.herrscher-sessions/<instanceID>/<name> on branch session/<instanceID>/<name>;
// with an empty instanceID (legacy) under <repo>/.herrscher-sessions/<name> on
// branch session/<name>, so multiple daemons sharing a repo never collide.
type Worktreer struct {
	ctx        context.Context
	instanceID string
}

// NewWorktreer builds a repo-stateless Worktreer, namespaced by instanceID
// ("" means legacy, non-namespaced layout).
func NewWorktreer(ctx context.Context, instanceID string) *Worktreer {
	return &Worktreer{ctx: ctx, instanceID: instanceID}
}

func (w *Worktreer) isGitRepo(repo string) bool {
	return exec.CommandContext(w.ctx, "git", "-C", repo, "rev-parse", "--git-dir").Run() == nil
}

// Path returns the on-disk worktree directory for a logical session name inside
// repo, namespaced by instanceID when set.
func (w *Worktreer) Path(repo, name string) string {
	return w.pathIn(repo, sessionsDir, name)
}

func (w *Worktreer) pathIn(repo, dir, name string) string {
	if w.instanceID == "" {
		return filepath.Join(repo, dir, name)
	}
	return filepath.Join(repo, dir, w.instanceID, name)
}

// Branch returns the git branch backing a logical session name, namespaced by
// instanceID when set.
func (w *Worktreer) Branch(name string) string { return branchFor(w.instanceID, name) }

func branchFor(instanceID, name string) string {
	if instanceID == "" {
		return "session/" + name
	}
	return "session/" + instanceID + "/" + name
}

// Create adds a worktree on branch session/<name> inside repo. When base is
// non-empty, the new branch starts at that ref (e.g. "session/<A>") so the
// worktree continues that tip without a merge; empty base keeps the default
// (branch from HEAD). Returns ("", nil) when repo is not a git repo (caller
// falls back to a shared session).
func (w *Worktreer) Create(repo, name, base string) (string, error) {
	if !w.isGitRepo(repo) {
		return "", nil
	}
	if base != "" && strings.HasPrefix(base, "-") {
		return "", fmt.Errorf("%w: %q", errFlagBase, base)
	}
	p := w.Path(repo, name)
	branch := w.Branch(name)
	switch reusable, err := w.reusable(repo, p, branch); {
	case err != nil:
		return "", err
	case reusable:
		return p, nil
	}
	args := []string{"-C", repo, subWorktree, "add", p, branch}
	if exists, err := w.BranchExistsAt(repo, branch); err != nil || !exists {
		args = []string{"-C", repo, subWorktree, "add", p, "-b", branch}
		if base != "" {
			args = append(args, base)
		}
	}
	out, err := exec.CommandContext(w.ctx, "git", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("worktree add: %s: %w", redact.Output(out), err)
	}
	return p, nil
}

// PreExisting reports whether a worktree for this session is already on disk, so
// a caller that rolls back on a later failure removes only what it created. A
// state it cannot verify counts as pre-existing: the cost of being wrong that
// way is a worktree left behind, and the cost of being wrong the other way is
// deleting a session's uncommitted work.
func (w *Worktreer) PreExisting(repo, name string) bool {
	p := w.Path(repo, name)
	if _, err := os.Lstat(p); err != nil {
		return !os.IsNotExist(err)
	}
	registered, err := w.registeredWorktree(repo, p)
	if err != nil {
		return true
	}
	return registered
}

func (w *Worktreer) reusable(repo, path, branch string) (bool, error) {
	registered, err := w.registeredWorktree(repo, path)
	if err != nil {
		return false, err
	}
	_, statErr := os.Lstat(path)
	missing := os.IsNotExist(statErr)

	if !registered {
		if statErr != nil && !missing {
			return false, fmt.Errorf("inspect %q: %w", path, statErr)
		}
		if !missing {
			return false, fmt.Errorf(
				"%q %w; move it aside to start this session", path, errNotAWorktree)
		}
		return false, nil
	}

	if missing {
		out, pruneErr := exec.CommandContext(w.ctx, "git", "-C", repo, subWorktree, "prune").CombinedOutput()
		if pruneErr != nil {
			return false, fmt.Errorf("worktree prune: %s: %w", redact.Output(out), pruneErr)
		}
		return false, nil
	}
	if statErr != nil {
		return false, fmt.Errorf("inspect %q: %w", path, statErr)
	}

	at, err := w.branchAt(path)
	if err != nil {
		return false, err
	}
	if at != branch {
		return false, fmt.Errorf(
			"%w %q: it is on branch %q, not %q; the session cannot take it over", errOtherBranch, path, at, branch)
	}
	return true, nil
}

func (w *Worktreer) branchAt(path string) (string, error) {
	out, err := exec.CommandContext(w.ctx, "git", "-C", path, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("cannot read the branch checked out at %q (detached HEAD?): %w", path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsCleanAt reports whether the worktree at path has no uncommitted changes. An
// error means the state can't be verified (missing dir, not a git repo), the
// caller must treat that as "not safe", never as clean.
func (w *Worktreer) IsCleanAt(path string) (bool, error) {
	out, err := exec.CommandContext(w.ctx, "git", "-C", path, "status", "--porcelain").Output()
	if err != nil {
		return false, fmt.Errorf("cannot verify clean state of %q: %w", path, err)
	}
	return strings.TrimSpace(string(out)) == "", nil
}

// BranchExistsAt reports whether branch exists as a local ref, querying the
// repo that contains path (an existing worktree dir inside that repo). Exit
// code 1 from "show-ref --verify" means the ref is simply absent. That is
// the normal "free to use" case, not an error. Any other failure (e.g. path
// isn't a git repo at all, exit 128) is reported as an error so callers never
// mistake "couldn't verify" for "free".
func (w *Worktreer) BranchExistsAt(path, branch string) (bool, error) {
	err := exec.CommandContext(w.ctx, "git", "-C", path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("cannot verify branch %q at %q: %w", branch, path, err)
}

// MergeOutcome classifies a MergeInto result.
type MergeOutcome int

const (
	MergeApplied MergeOutcome = iota
	MergeUpToDate
	MergeConflict
)

// MergeInto runs `git -C leadPath merge --no-edit branch`. On success it reports
// MergeApplied (a commit was created) or MergeUpToDate (HEAD unchanged). On a
// conflict it returns MergeConflict plus the conflicted file list and runs
// `git merge --abort`, so leadPath is left clean either way. Any other git
// failure is returned as an error (with a best-effort abort first, so a
// half-started merge never lingers); when err != nil the MergeOutcome is
// undefined and callers must branch on err first.
func (w *Worktreer) MergeInto(leadPath, branch string) (MergeOutcome, []string, error) {
	pre, err := w.headAt(leadPath)
	if err != nil {
		return MergeConflict, nil, fmt.Errorf("merge: cannot read HEAD of %q: %w", leadPath, err)
	}
	out, mergeErr := exec.CommandContext(w.ctx, "git", "-C", leadPath, "merge", "--no-edit", branch).CombinedOutput()
	if mergeErr == nil {
		post, err := w.headAt(leadPath)
		if err != nil {
			return MergeApplied, nil, fmt.Errorf("merge: cannot read HEAD of %q after merge: %w", leadPath, err)
		}
		if post == pre {
			return MergeUpToDate, nil, nil
		}
		return MergeApplied, nil, nil
	}
	conflicts := w.conflictFiles(leadPath)
	logFailure("merge --abort", exec.CommandContext(w.ctx, "git", "-C", leadPath, "merge", "--abort").Run())
	if len(conflicts) > 0 {
		return MergeConflict, conflicts, nil
	}
	return MergeConflict, nil, fmt.Errorf("merge %s into %q: %s: %w", branch, leadPath, redact.Output(out), mergeErr)
}

func (w *Worktreer) headAt(path string) (string, error) {
	out, err := exec.CommandContext(w.ctx, "git", "-C", path, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (w *Worktreer) conflictFiles(path string) []string {
	out, err := exec.CommandContext(w.ctx, "git", "-C", path, "diff", "--name-only", "--diff-filter=U").Output()
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// Remove removes the worktree. If it has uncommitted changes and !force, it
// refuses with the status. The branch session/<name> is always left intact.
func (w *Worktreer) Remove(repo, name string, force bool) error {
	p := w.Path(repo, name)
	if !force {
		clean, err := w.IsCleanAt(p)
		if err != nil {
			return fmt.Errorf("worktree %q: %w; close with force:true to remove anyway", name, err)
		}
		if !clean {
			return fmt.Errorf("worktree %q has %w", name, errUncommitted)
		}
	}
	args := []string{"-C", repo, subWorktree, "remove", p}
	if force {
		args = append(args, "--force")
	}
	out, err := exec.CommandContext(w.ctx, "git", args...).CombinedOutput()
	if err != nil {
		removeErr := fmt.Errorf("worktree remove: %s: %w", redact.Output(out), err)
		if force {
			recovered, recoverErr := w.recoverEmptyOrphan(repo, name)
			if recoverErr != nil {
				return fmt.Errorf("%w; empty-orphan recovery: %w", removeErr, recoverErr)
			}
			if recovered {
				return nil
			}
		}
		return removeErr
	}
	return nil
}

func (w *Worktreer) recoverEmptyOrphan(repo, name string) (bool, error) {
	path, ok := w.validSessionChild(repo, name)
	if !ok {
		return false, nil
	}
	registered, err := w.registeredWorktree(repo, path)
	if err != nil {
		return false, err
	}
	if registered {
		return false, nil
	}
	entries, err := os.ReadDir(path)
	switch {
	case os.IsNotExist(err):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("inspect %q: %w", path, err)
	case len(entries) != 0:
		return false, nil
	}
	revalidated, ok := w.validSessionChild(repo, name)
	if !ok || !samePath(revalidated, path) {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("remove empty orphan %q: %w", path, err)
	}
	return true, nil
}

func (w *Worktreer) validSessionChild(repo, name string) (string, bool) {
	if !validPathComponent(name) ||
		(w.instanceID != "" && !validPathComponent(w.instanceID)) {
		return "", false
	}
	repo, err := filepath.Abs(repo)
	if err != nil {
		return "", false
	}
	resolvedRepo, err := resolveExistingPath(repo)
	if err != nil {
		return "", false
	}

	root := filepath.Join(repo, sessionsDir)
	expectedRoot := filepath.Join(resolvedRepo, sessionsDir)
	if w.instanceID != "" {
		root = filepath.Join(root, w.instanceID)
		expectedRoot = filepath.Join(expectedRoot, w.instanceID)
	}
	resolvedRoot, err := resolveExistingPath(root)
	if err != nil || !samePath(resolvedRoot, expectedRoot) {
		return "", false
	}

	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return path, true
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	resolvedPath, err := resolveExistingPath(path)
	if err != nil || !samePath(resolvedPath, filepath.Join(resolvedRoot, name)) {
		return "", false
	}
	return path, true
}

func validPathComponent(value string) bool {
	return value != "" &&
		value != "." &&
		value != ".." &&
		filepath.Base(value) == value
}

func (w *Worktreer) registeredWorktree(repo, target string) (bool, error) {
	out, err := exec.CommandContext(
		w.ctx,
		"git", "-C", repo, subWorktree, "list", "--porcelain", "-z",
	).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("list registered worktrees: %s: %w", redact.Output(out), err)
	}
	rawTarget := target
	target, err = canonicalPathForComparison(target)
	if err != nil {
		return false, fmt.Errorf("resolve target worktree path %q: %w", rawTarget, err)
	}
	for _, field := range bytes.Split(out, []byte{0}) {
		const prefix = "worktree "
		if !bytes.HasPrefix(field, []byte(prefix)) {
			continue
		}
		path, err := canonicalPathForComparison(
			filepath.FromSlash(string(field[len(prefix):])),
		)
		if err != nil {
			return false, fmt.Errorf("resolve registered worktree path: %w", err)
		}
		if samePath(path, target) {
			return true, nil
		}
	}
	return false, nil
}

func canonicalPathForComparison(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var absent []string
	for {
		resolved, err := resolveExistingPath(path)
		if err == nil {
			return filepath.Join(append([]string{resolved}, absent...)...), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return filepath.Join(append([]string{path}, absent...)...), nil
		}
		absent = append([]string{filepath.Base(path)}, absent...)
		path = parent
	}
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
