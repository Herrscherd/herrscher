package worktree

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/shellquote"
)

const gitlinkMode = "160000"

func (w *Worktreer) checkMovable(ctx context.Context, plan Relocation) error {
	locked, err := w.lockedWorktree(ctx, plan)
	if err != nil {
		return err
	}
	if locked {
		return NewFailure(w.messages, clitext.WorktreeRelocateLocked, clitext.Params{
			clitext.ParamName:    strconv.Quote(plan.Name),
			clitext.ParamPath:    strconv.Quote(plan.FromPath),
			clitext.ParamCommand: "git -C " + shellquote.Quote(plan.Repo) + " worktree unlock " + shellquote.Quote(plan.FromPath),
		}, nil)
	}
	submodules, err := w.holdsSubmodules(ctx, plan.FromPath)
	if err != nil {
		return err
	}
	if submodules {
		return NewFailure(w.messages, clitext.WorktreeRelocateSubmodules, clitext.Params{
			clitext.ParamName:    strconv.Quote(plan.Name),
			clitext.ParamPath:    strconv.Quote(plan.FromPath),
			clitext.ParamCommand: manualSubmoduleRelocation(plan),
		}, nil)
	}
	return nil
}

func (w *Worktreer) lockedWorktree(ctx context.Context, plan Relocation) (bool, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", plan.Repo, subWorktree, "list", "--porcelain").Output()
	if err != nil {
		return false, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(plan.Repo), clitext.ParamErr: err}, err)
	}
	want := samePathAs(plan.FromPath)
	current := false
	for line := range strings.Lines(string(out)) {
		line = strings.TrimRight(line, "\n")
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			current = want(path)
			continue
		}
		if current && (line == "locked" || strings.HasPrefix(line, "locked ")) {
			return true, nil
		}
	}
	return false, nil
}

func samePathAs(path string) func(string) bool {
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		resolved = clean
	}
	return func(candidate string) bool {
		candidate = filepath.Clean(candidate)
		return candidate == clean || candidate == resolved
	}
}

func (w *Worktreer) holdsSubmodules(ctx context.Context, path string) (bool, error) {
	held, err := submodulesHeld(ctx, path)
	if err != nil {
		return false, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(path), clitext.ParamErr: err}, err)
	}
	return held, nil
}

func submodulesHeld(ctx context.Context, path string) (bool, error) {
	modules, err := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--path-format=absolute", "--git-path", "modules").Output()
	if err != nil {
		return false, err
	}
	modulesPresent, err := exists(strings.TrimSpace(string(modules)))
	if err != nil || modulesPresent {
		return modulesPresent, err
	}
	staged, err := exec.CommandContext(ctx, "git", "-C", path, "ls-files", "--stage", "-z").Output()
	if err != nil {
		return false, err
	}
	for entry := range strings.SplitSeq(string(staged), "\x00") {
		meta, name, found := strings.Cut(entry, "\t")
		if !found || !strings.HasPrefix(meta, gitlinkMode+" ") {
			continue
		}
		populated, statErr := exists(filepath.Join(path, name, ".git"))
		if statErr != nil || populated {
			return populated, statErr
		}
	}
	return false, nil
}

func manualSubmoduleRelocation(plan Relocation) string {
	quote := shellquote.Quote
	steps := []string{
		"mkdir -p " + quote(filepath.Dir(plan.ToPath)),
		"mv " + quote(plan.FromPath) + " " + quote(plan.ToPath),
		"git -C " + quote(plan.Repo) + " worktree repair " + quote(plan.ToPath),
	}
	if plan.moveBranch {
		steps = append(steps, "git -C "+quote(plan.Repo)+" branch -m "+quote(plan.FromBranch)+" "+quote(plan.ToBranch))
	}
	steps = append(steps, "git -C "+quote(plan.ToPath)+" submodule absorbgitdirs")
	return strings.Join(steps, " && ")
}
