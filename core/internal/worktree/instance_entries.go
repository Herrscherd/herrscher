package worktree

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/redact"
)

type registeredEntry struct {
	path   string
	branch string
}

// ForeignInstanceEntries lists the worktrees git registers under the instance folder of repo, and the branches under the instance branch folder, that are none of owned: the worktree paths and branches the sessions of this state hold. A branch checked out in an owned worktree is owned too.
func (w *Worktreer) ForeignInstanceEntries(ctx context.Context, repo string, ownedPaths, ownedBranches []string) ([]string, error) {
	entries, err := w.registeredEntries(ctx, repo)
	if err != nil {
		return nil, err
	}
	folder, err := canonicalPathForComparison(filepath.Join(repo, sessionsDir, w.instanceID))
	if err != nil {
		return nil, NewFailure(w.messages, clitext.WorktreeResolveTarget, clitext.Params{clitext.ParamPath: strconv.Quote(repo), clitext.ParamErr: err}, err)
	}
	owned, err := canonicalPaths(ownedPaths)
	if err != nil {
		return nil, NewFailure(w.messages, clitext.WorktreeResolveRegistered, clitext.Params{clitext.ParamErr: err}, err)
	}
	branches := make(map[string]bool, len(ownedBranches))
	for _, branch := range ownedBranches {
		branches[branch] = true
	}
	var foreign []string
	for _, entry := range entries {
		if owned[entry.path] {
			branches[entry.branch] = true
			continue
		}
		if strings.HasPrefix(entry.path, folder+string(filepath.Separator)) {
			foreign = append(foreign, entry.path)
		}
	}
	foreignBranches, err := w.foreignInstanceBranches(ctx, repo, branches)
	if err != nil {
		return nil, err
	}
	return append(foreign, foreignBranches...), nil
}

func (w *Worktreer) foreignInstanceBranches(ctx context.Context, repo string, owned map[string]bool) ([]string, error) {
	prefix := "refs/heads/" + branchPrefix + w.instanceID + "/"
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "for-each-ref", "--format=%(refname)", prefix).CombinedOutput()
	if err != nil {
		return nil, NewFailure(w.messages, clitext.WorktreeListFailed, clitext.Params{clitext.ParamOutput: redact.Output(out), clitext.ParamErr: err}, err)
	}
	var foreign []string
	for ref := range strings.Lines(string(out)) {
		branch := strings.TrimPrefix(strings.TrimSpace(ref), "refs/heads/")
		if branch != "" && !owned[branch] {
			foreign = append(foreign, branch)
		}
	}
	return foreign, nil
}

func (w *Worktreer) registeredEntries(ctx context.Context, repo string) ([]registeredEntry, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repo, subWorktree, "list", "--porcelain", "-z").CombinedOutput()
	if err != nil {
		return nil, NewFailure(w.messages, clitext.WorktreeListFailed, clitext.Params{clitext.ParamOutput: redact.Output(out), clitext.ParamErr: err}, err)
	}
	var entries []registeredEntry
	for _, field := range bytes.Split(out, []byte{0}) {
		line := string(field)
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			canonical, resolveErr := canonicalPathForComparison(filepath.FromSlash(path))
			if resolveErr != nil {
				return nil, NewFailure(w.messages, clitext.WorktreeResolveRegistered, clitext.Params{clitext.ParamErr: resolveErr}, resolveErr)
			}
			entries = append(entries, registeredEntry{path: canonical})
			continue
		}
		if branch, ok := strings.CutPrefix(line, "branch refs/heads/"); ok && len(entries) > 0 {
			entries[len(entries)-1].branch = branch
		}
	}
	return entries, nil
}

func canonicalPaths(paths []string) (map[string]bool, error) {
	canonical := make(map[string]bool, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		resolved, err := canonicalPathForComparison(path)
		if err != nil {
			return nil, err
		}
		canonical[resolved] = true
	}
	return canonical, nil
}
