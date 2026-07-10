package worktree

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktreer manages git worktrees. It is repo-stateless: the repo root is passed
// to each method, so one Worktreer serves every project in the workspace.
// With a non-empty instanceID, worktrees live under
// <repo>/.dctl-sessions/<instanceID>/<name> on branch session/<instanceID>/<name>;
// with an empty instanceID (legacy) under <repo>/.dctl-sessions/<name> on
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
	if w.instanceID == "" {
		return filepath.Join(repo, ".dctl-sessions", name)
	}
	return filepath.Join(repo, ".dctl-sessions", w.instanceID, name)
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
		return "", fmt.Errorf("worktree add: %s", strings.TrimSpace(string(out)))
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
		return fmt.Errorf("worktree remove: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
