package worktree

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
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
	args := []string{"-C", repo, "worktree", "add", p, "-b", w.Branch(name)}
	if base != "" {
		args = append(args, base)
	}
	out, err := exec.CommandContext(w.ctx, "git", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("worktree add: %s", redact.Output(out))
	}
	return p, nil
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
		return fmt.Errorf("worktree remove: %s", redact.Output(out))
	}
	return nil
}
