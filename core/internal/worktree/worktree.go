// Package worktree manages the git worktrees that give each session its own working copy.
package worktree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/redact"
)

const (
	subWorktree = "worktree"
)

const sessionsDir = ".herrscher-sessions"

// Worktreer manages git worktrees. It is repo-stateless: the repo root is passed
// to each method, so one Worktreer serves every project in the workspace.
// Worktrees live under <repo>/.herrscher-sessions/<instanceID>/<name> on branch
// session/<instanceID>/<name>, so multiple daemons sharing a repo never collide.
type Worktreer struct {
	messages   *clitext.Messages
	instanceID string
}

// NewWorktreer builds a repo-stateless Worktreer, namespaced by instanceID.
func NewWorktreer(messages *clitext.Messages, instanceID string) *Worktreer {
	return &Worktreer{messages: messages, instanceID: instanceID}
}

func (w *Worktreer) isGitRepo(ctx context.Context, repo string) bool {
	return exec.CommandContext(ctx, "git", "-C", repo, "rev-parse", "--git-dir").Run() == nil
}

// Path returns the on-disk worktree directory for a logical session name inside
// repo, namespaced by instanceID.
func (w *Worktreer) Path(repo, name string) string {
	return w.pathIn(repo, sessionsDir, name)
}

func (w *Worktreer) pathIn(repo, dir, name string) string {
	return filepath.Join(repo, dir, w.instanceID, name)
}

// Branch returns the git branch backing a logical session name, namespaced by
// instanceID.
func (w *Worktreer) Branch(name string) string { return branchFor(w.instanceID, name) }

func branchFor(instanceID, name string) string {
	return branchPrefix + instanceID + "/" + name
}

const branchPrefix = "session/"

// Create adds a worktree on branch session/<name> inside repo. When base is
// non-empty, the new branch starts at that ref (e.g. "session/<A>") so the
// worktree continues that tip without a merge; empty base keeps the default
// (branch from HEAD). Returns ("", nil) when repo is not a git repo (caller
// falls back to a shared session).
func (w *Worktreer) Create(ctx context.Context, repo, name, base string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !w.isGitRepo(ctx, repo) {
		return "", nil
	}
	if base != "" && strings.HasPrefix(base, "-") {
		return "", NewFailure(w.messages, clitext.WorktreeFlagBase, clitext.Params{clitext.ParamRef: strconv.Quote(base)}, nil)
	}
	wtPath := w.Path(repo, name)
	branch := w.Branch(name)
	switch reusable, err := w.reusable(ctx, repo, wtPath, branch); {
	case err != nil:
		return "", err
	case reusable:
		return wtPath, nil
	}
	args := []string{"-C", repo, subWorktree, "add", wtPath, branch}
	if exists, err := w.BranchExistsAt(ctx, repo, branch); err != nil || !exists {
		args = []string{"-C", repo, subWorktree, "add", wtPath, "-b", branch}
		if base != "" {
			args = append(args, base)
		}
	}
	out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
	if err != nil {
		return "", NewFailure(w.messages, clitext.WorktreeAddFailed, clitext.Params{clitext.ParamOutput: redact.Output(out), clitext.ParamErr: err}, err)
	}
	return wtPath, nil
}

// PreExisting reports whether a worktree for this session is already on disk, so
// a caller that rolls back on a later failure removes only what it created. A
// state it cannot verify counts as pre-existing: the cost of being wrong that
// way is a worktree left behind, and the cost of being wrong the other way is
// deleting a session's uncommitted work.
func (w *Worktreer) PreExisting(ctx context.Context, repo, name string) bool {
	wtPath := w.Path(repo, name)
	if _, err := os.Lstat(wtPath); err != nil {
		return !os.IsNotExist(err)
	}
	registered, err := w.registeredWorktree(ctx, repo, wtPath)
	if err != nil {
		return true
	}
	return registered
}

func (w *Worktreer) reusable(ctx context.Context, repo, path, branch string) (bool, error) {
	registered, err := w.registeredWorktree(ctx, repo, path)
	if err != nil {
		return false, err
	}
	_, statErr := os.Lstat(path)
	missing := os.IsNotExist(statErr)

	if !registered {
		if statErr != nil && !missing {
			return false, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(path), clitext.ParamErr: statErr}, statErr)
		}
		if !missing {
			return false, NewFailure(w.messages, clitext.WorktreeNotAWorktree, clitext.Params{clitext.ParamPath: strconv.Quote(path)}, nil)
		}
		return false, nil
	}

	if missing {
		out, pruneErr := exec.CommandContext(ctx, "git", "-C", repo, subWorktree, "prune").CombinedOutput()
		if pruneErr != nil {
			return false, NewFailure(w.messages, clitext.WorktreePruneFailed, clitext.Params{clitext.ParamOutput: redact.Output(out), clitext.ParamErr: pruneErr}, pruneErr)
		}
		return false, nil
	}
	if statErr != nil {
		return false, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(path), clitext.ParamErr: statErr}, statErr)
	}

	tip, err := w.branchAt(ctx, path)
	if err != nil {
		return false, err
	}
	if tip != branch {
		return false, NewFailure(w.messages, clitext.WorktreeOtherBranch, clitext.Params{clitext.ParamPath: strconv.Quote(path), clitext.ParamTip: strconv.Quote(tip), clitext.ParamBranch: strconv.Quote(branch)}, nil)
	}
	return true, nil
}

func (w *Worktreer) branchAt(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", path, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return "", NewFailure(w.messages, clitext.WorktreeBranchUnreadable, clitext.Params{clitext.ParamPath: strconv.Quote(path), clitext.ParamErr: err}, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsCleanAt reports whether the worktree at path has no uncommitted changes. An
// error means the state can't be verified (missing dir, not a git repo), the
// caller must treat that as "not safe", never as clean.
func (w *Worktreer) IsCleanAt(ctx context.Context, path string) (bool, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", path, "status", "--porcelain").Output()
	if err != nil {
		return false, NewFailure(w.messages, clitext.WorktreeCleanUnverified, clitext.Params{clitext.ParamPath: strconv.Quote(path), clitext.ParamErr: err}, err)
	}
	return strings.TrimSpace(string(out)) == "", nil
}

// BranchExistsAt reports whether branch exists as a local ref, querying the
// repo that contains path (an existing worktree dir inside that repo). Exit
// code 1 from "show-ref --verify" means the ref is simply absent. That is
// the normal "free to use" case, not an error. Any other failure (e.g. path
// isn't a git repo at all, exit 128) is reported as an error so callers never
// mistake "couldn't verify" for "free".
func (w *Worktreer) BranchExistsAt(ctx context.Context, path, branch string) (bool, error) {
	err := exec.CommandContext(ctx, "git", "-C", path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, NewFailure(w.messages, clitext.WorktreeBranchUnverified, clitext.Params{clitext.ParamBranch: strconv.Quote(branch), clitext.ParamPath: strconv.Quote(path), clitext.ParamErr: err}, err)
}

// MergeOutcome classifies a MergeInto result.
type MergeOutcome int

const (
	// MergeApplied reports that the merge was applied.
	MergeApplied MergeOutcome = iota
	// MergeUpToDate reports that the branch already contained the merged content.
	MergeUpToDate
	// MergeConflict reports that the merge stopped on conflicting files.
	MergeConflict
)

// MergeInto runs `git -C leadPath merge --no-edit branch`. On success it reports
// MergeApplied (a commit was created) or MergeUpToDate (HEAD unchanged). On a
// conflict it returns MergeConflict plus the conflicted file list and runs
// `git merge --abort`, so leadPath is left clean either way. Any other git
// failure is returned as an error (with a best-effort abort first, so a
// half-started merge never lingers); when err != nil the MergeOutcome is
// undefined and callers must branch on err first.
func (w *Worktreer) MergeInto(ctx context.Context, leadPath, branch string) (MergeOutcome, []string, error) {
	pre, err := w.headAt(ctx, leadPath)
	if err != nil {
		return MergeConflict, nil, NewFailure(w.messages, clitext.WorktreeHeadUnreadable, clitext.Params{clitext.ParamPath: strconv.Quote(leadPath), clitext.ParamErr: err}, err)
	}
	out, mergeErr := exec.CommandContext(ctx, "git", "-C", leadPath, "merge", "--no-edit", branch).CombinedOutput()
	if mergeErr == nil {
		post, err := w.headAt(ctx, leadPath)
		if err != nil {
			return MergeApplied, nil, NewFailure(w.messages, clitext.WorktreeHeadUnreadableAfterMerge, clitext.Params{clitext.ParamPath: strconv.Quote(leadPath), clitext.ParamErr: err}, err)
		}
		if post == pre {
			return MergeUpToDate, nil, nil
		}
		return MergeApplied, nil, nil
	}
	conflicts := w.conflictFiles(ctx, leadPath)
	logFailure("merge --abort", exec.CommandContext(ctx, "git", "-C", leadPath, "merge", "--abort").Run())
	if len(conflicts) > 0 {
		return MergeConflict, conflicts, nil
	}
	return MergeConflict, nil, NewFailure(w.messages, clitext.WorktreeMergeFailed, clitext.Params{clitext.ParamBranch: branch, clitext.ParamPath: strconv.Quote(leadPath), clitext.ParamOutput: redact.Output(out), clitext.ParamErr: mergeErr}, mergeErr)
}

func (w *Worktreer) headAt(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (w *Worktreer) conflictFiles(ctx context.Context, path string) []string {
	out, err := exec.CommandContext(ctx, "git", "-C", path, "diff", "--name-only", "--diff-filter=U").Output()
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
func (w *Worktreer) Remove(ctx context.Context, repo, name string, force bool) error {
	wtPath := w.Path(repo, name)
	if !force {
		clean, err := w.IsCleanAt(ctx, wtPath)
		if err != nil {
			return NewFailure(w.messages, clitext.WorktreeUncommittedRemove, clitext.Params{clitext.ParamName: strconv.Quote(name), clitext.ParamErr: err}, err)
		}
		if !clean {
			return NewFailure(w.messages, clitext.WorktreeUncommitted, clitext.Params{clitext.ParamName: strconv.Quote(name)}, nil)
		}
	}
	args := []string{"-C", repo, subWorktree, "remove", wtPath}
	if force {
		args = append(args, "--force")
	}
	out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
	if err != nil {
		removeErr := NewFailure(w.messages, clitext.WorktreeRemoveFailed, clitext.Params{clitext.ParamOutput: redact.Output(out), clitext.ParamErr: err}, err)
		if force {
			recovered, recoverErr := w.recoverEmptyOrphan(ctx, repo, name)
			if recoverErr != nil {
				return NewFailure(w.messages, clitext.WorktreeOrphanRecovery, clitext.Params{clitext.ParamRemove: removeErr, clitext.ParamRecovery: recoverErr}, recoverErr)
			}
			if recovered {
				return nil
			}
		}
		return removeErr
	}
	return nil
}

func (w *Worktreer) recoverEmptyOrphan(ctx context.Context, repo, name string) (bool, error) {
	path, ok := w.validSessionChild(repo, name)
	if !ok {
		return false, nil
	}
	registered, err := w.registeredWorktree(ctx, repo, path)
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
		return false, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(path), clitext.ParamErr: err}, err)
	case len(entries) != 0:
		return false, nil
	}
	revalidated, ok := w.validSessionChild(repo, name)
	if !ok || !samePath(revalidated, path) {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, NewFailure(w.messages, clitext.WorktreeOrphanRemove, clitext.Params{clitext.ParamPath: strconv.Quote(path), clitext.ParamErr: err}, err)
	}
	return true, nil
}

func (w *Worktreer) validSessionChild(repo, name string) (string, bool) {
	if !validPathComponent(name) || !validPathComponent(w.instanceID) {
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

	root := filepath.Join(repo, sessionsDir, w.instanceID)
	expectedRoot := filepath.Join(resolvedRepo, sessionsDir, w.instanceID)
	resolvedRoot, err := resolveExistingPath(root)
	if os.IsNotExist(err) {
		return filepath.Join(root, name), true
	}
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

func (w *Worktreer) registeredWorktree(ctx context.Context, repo, target string) (bool, error) {
	out, err := exec.CommandContext(
		ctx,
		"git", "-C", repo, subWorktree, "list", "--porcelain", "-z",
	).CombinedOutput()
	if err != nil {
		return false, NewFailure(w.messages, clitext.WorktreeListFailed, clitext.Params{clitext.ParamOutput: redact.Output(out), clitext.ParamErr: err}, err)
	}
	rawTarget := target
	target, err = canonicalPathForComparison(target)
	if err != nil {
		return false, NewFailure(w.messages, clitext.WorktreeResolveTarget, clitext.Params{clitext.ParamPath: strconv.Quote(rawTarget), clitext.ParamErr: err}, err)
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
			return false, NewFailure(w.messages, clitext.WorktreeResolveRegistered, clitext.Params{clitext.ParamErr: err}, err)
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

func samePath(other, right string) bool {
	other, right = filepath.Clean(other), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(other, right)
	}
	return other == right
}
