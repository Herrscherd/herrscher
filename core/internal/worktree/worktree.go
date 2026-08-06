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

// sessionsDir is the per-repo directory that holds session worktrees.
const sessionsDir = ".herrscher-sessions"

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
func (w *Worktreer) Branch(name string) string {
	if w.instanceID == "" {
		return "session/" + name
	}
	return "session/" + w.instanceID + "/" + name
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
	// base is passed as a positional git arg; a leading "-" would let a
	// caller-supplied ref be parsed as a git flag (option injection). The
	// handoff path only ever passes safe "session/<A>" refs, but the
	// operator-CLI base param is arbitrary, so guard here defense-in-depth.
	if base != "" && strings.HasPrefix(base, "-") {
		return "", fmt.Errorf("worktree: refusing base ref %q that looks like a flag", base)
	}
	p := w.Path(repo, name)
	// Closing a session removes its worktree but keeps its branch — the branch is
	// where the committed, unmerged work lives, and deleting it would throw that
	// away. So a name that comes back finds its branch already there. Check it out
	// instead of asking for a new one: a session named after a channel is exactly
	// the case that comes back, and it should return to its own work rather than
	// refuse to start. base only decides where a *new* branch begins; a branch
	// that already exists carries its own history.
	branch := w.Branch(name)
	// The same reasoning applies to the directory. Closing a session removes its
	// worktree, but a crash, a kill, or a daemon restart mid-session leaves the
	// directory registered and on its branch — and then the session that comes
	// back is refused by `worktree add` on the grounds that its own worktree is
	// in the way. Reuse it: a registered worktree already on the right branch is
	// not an obstacle to the session, it *is* the session.
	switch reusable, err := w.reusable(repo, p, branch); {
	case err != nil:
		return "", err
	case reusable:
		return p, nil
	}
	args := []string{"-C", repo, "worktree", "add", p, branch}
	if exists, err := w.BranchExistsAt(repo, branch); err != nil || !exists {
		args = []string{"-C", repo, "worktree", "add", p, "-b", branch}
		if base != "" {
			args = append(args, base)
		}
	}
	out, err := exec.CommandContext(w.ctx, "git", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("worktree add: %s", redact.Output(out))
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

// reusable reports whether the worktree at path can be handed back to a session
// asking for branch, and repairs the states that are merely stale.
//
// It never deletes anything with content. A directory sitting where the worktree
// belongs, but which git does not know, is someone else's — reporting that is
// the whole value of the check, since removing it is how a session would eat
// work nobody asked it to touch.
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
				"%q already exists and is not a git worktree — move it aside to start this session", path)
		}
		return false, nil
	}

	// Registered but gone from disk: a worktree removed by hand, or a session
	// directory lost with the filesystem under it. git holds the registration and
	// would refuse the add on its own record alone, so drop the record.
	if missing {
		if out, err := exec.CommandContext(w.ctx, "git", "-C", repo, "worktree", "prune").CombinedOutput(); err != nil {
			return false, fmt.Errorf("worktree prune: %s", redact.Output(out))
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
			"worktree %q is on branch %q, not %q — the session cannot take it over", path, at, branch)
	}
	return true, nil
}

// branchAt names the branch checked out in an existing worktree. A detached HEAD
// has no branch to compare against, which is reported rather than guessed at.
func (w *Worktreer) branchAt(path string) (string, error) {
	out, err := exec.CommandContext(w.ctx, "git", "-C", path, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("cannot read the branch checked out at %q (detached HEAD?): %w", path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsCleanAt reports whether the worktree at path has no uncommitted changes. An
// error means the state can't be verified (missing dir, not a git repo) — the
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
// code 1 from "show-ref --verify" means the ref is simply absent — that is
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
	MergeApplied  MergeOutcome = iota // a merge commit was created
	MergeUpToDate                     // nothing to merge — branch already in lead
	MergeConflict                     // conflict — the merge was aborted
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
	// Merge failed. If files are in conflict, abort and report them cleanly.
	conflicts := w.conflictFiles(leadPath)
	_ = exec.CommandContext(w.ctx, "git", "-C", leadPath, "merge", "--abort").Run()
	if len(conflicts) > 0 {
		return MergeConflict, conflicts, nil
	}
	return MergeConflict, nil, fmt.Errorf("merge %s into %q: %s", branch, leadPath, redact.Output(out))
}

// headAt returns the commit HEAD points to in the given worktree.
func (w *Worktreer) headAt(path string) (string, error) {
	out, err := exec.CommandContext(w.ctx, "git", "-C", path, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// conflictFiles lists paths currently in an unmerged (conflicted) state.
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
			// Can't verify the tree is clean: refuse rather than risk discarding
			// work. force: bypasses.
			return fmt.Errorf("worktree %q: %v — close with force:true to remove anyway", name, err)
		}
		if !clean {
			return fmt.Errorf("worktree %q has uncommitted changes", name)
		}
	}
	args := []string{"-C", repo, "worktree", "remove", p}
	if force {
		args = append(args, "--force")
	}
	out, err := exec.CommandContext(w.ctx, "git", args...).CombinedOutput()
	if err != nil {
		removeErr := fmt.Errorf("worktree remove: %s", redact.Output(out))
		if force {
			recovered, recoverErr := w.recoverEmptyOrphan(repo, name)
			if recoverErr != nil {
				return fmt.Errorf("%v; empty-orphan recovery: %w", removeErr, recoverErr)
			}
			if recovered {
				return nil
			}
		}
		return removeErr
	}
	return nil
}

// recoverEmptyOrphan handles the partial Windows failure where `git worktree
// remove` already deleted its registration and contents, but a process-held
// directory handle prevented the now-empty root directory from being removed.
// It removes exactly one validated empty session directory, never recursively.
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
	// Revalidate after the external git/list/read operations so a path swapped
	// for a link in the meantime is never handed directly to os.Remove.
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
		"git", "-C", repo, "worktree", "list", "--porcelain", "-z",
	).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("list registered worktrees: %s", redact.Output(out))
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

// canonicalPathForComparison resolves links in the part of a path that exists,
// then restores the components that do not. Any number of trailing components
// may be absent: the sessions directory itself does not exist before the repo's
// first session, and a partial removal can take more than the leaf with it.
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
			// The root itself does not resolve: nothing of this path exists, so
			// there is nothing to canonicalise and the path stands for itself.
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
