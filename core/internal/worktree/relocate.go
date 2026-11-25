package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/redact"
)

const relocatedDirMode = 0o750

// Relocation moves one session out of the layout a daemon without an instance id used.
type Relocation struct {
	Name       string
	Repo       string
	FromPath   string
	ToPath     string
	FromBranch string
	ToBranch   string
	Scratch    bool
	moveDir    bool
	moveBranch bool
}

// Pending reports whether the relocation still has anything to do on disk.
func (r Relocation) Pending() bool { return r.moveDir || r.moveBranch }

// PlanBareRelocation reports where the session worktree or scratch directory at path, in the id-less layout, moves to under this instance.
func (w *Worktreer) PlanBareRelocation(ctx context.Context, name, path string, scratch bool) (Relocation, error) {
	if !InBareLayout(name, path, scratch) {
		return Relocation{}, NewFailure(w.messages, clitext.WorktreeNotAWorktree, clitext.Params{clitext.ParamPath: strconv.Quote(path)}, nil)
	}
	repo := filepath.Dir(filepath.Dir(filepath.Clean(path)))
	plan := Relocation{Name: name, Repo: repo, FromPath: filepath.Clean(path), Scratch: scratch}
	if scratch {
		plan.ToPath = w.ScratchPath(repo, name)
	} else {
		plan.ToPath = w.Path(repo, name)
		plan.FromBranch, plan.ToBranch = branchPrefix+name, w.Branch(name)
	}
	moveDir, err := w.planDirMove(plan)
	if err != nil {
		return Relocation{}, err
	}
	plan.moveDir = moveDir
	if scratch {
		return plan, nil
	}
	if moveDir {
		if branchErr := w.checkOnBranch(ctx, plan.FromPath, plan.FromBranch); branchErr != nil {
			return Relocation{}, branchErr
		}
	}
	moveBranch, err := w.planBranchRename(ctx, plan)
	if err != nil {
		return Relocation{}, err
	}
	plan.moveBranch = moveBranch
	return plan, nil
}

func (w *Worktreer) planDirMove(plan Relocation) (bool, error) {
	fromExists, err := exists(plan.FromPath)
	if err != nil {
		return false, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(plan.FromPath), clitext.ParamErr: err}, err)
	}
	toExists, err := exists(plan.ToPath)
	if err != nil {
		return false, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(plan.ToPath), clitext.ParamErr: err}, err)
	}
	if fromExists && toExists {
		return false, NewFailure(w.messages, clitext.WorktreeRelocateTargetExists, clitext.Params{clitext.ParamName: strconv.Quote(plan.Name), clitext.ParamPath: strconv.Quote(plan.ToPath), clitext.ParamFrom: strconv.Quote(plan.FromPath)}, nil)
	}
	return fromExists, nil
}

func (w *Worktreer) checkOnBranch(ctx context.Context, path, branch string) error {
	tip, err := w.branchAt(ctx, path)
	if err != nil {
		return err
	}
	if tip != branch {
		return NewFailure(w.messages, clitext.WorktreeOtherBranch, clitext.Params{clitext.ParamPath: strconv.Quote(path), clitext.ParamTip: strconv.Quote(tip), clitext.ParamBranch: strconv.Quote(branch)}, nil)
	}
	return nil
}

func (w *Worktreer) planBranchRename(ctx context.Context, plan Relocation) (bool, error) {
	fromExists, err := w.BranchExistsAt(ctx, plan.Repo, plan.FromBranch)
	if err != nil {
		return false, err
	}
	toExists, err := w.BranchExistsAt(ctx, plan.Repo, plan.ToBranch)
	if err != nil {
		return false, err
	}
	if !fromExists {
		return false, nil
	}
	if toExists {
		return false, NewFailure(w.messages, clitext.WorktreeRelocateBranchExists, clitext.Params{clitext.ParamName: strconv.Quote(plan.Name), clitext.ParamBranch: strconv.Quote(plan.ToBranch), clitext.ParamFrom: strconv.Quote(plan.FromBranch)}, nil)
	}
	upstream, err := exec.CommandContext(ctx, "git", "-C", plan.Repo, "config", "--get", "branch."+plan.FromBranch+".merge").Output()
	if err == nil && strings.TrimSpace(string(upstream)) != "" {
		return false, NewFailure(w.messages, clitext.WorktreeRelocateUpstream, clitext.Params{clitext.ParamName: strconv.Quote(plan.Name), clitext.ParamBranch: strconv.Quote(plan.FromBranch)}, nil)
	}
	return true, nil
}

// Relocate carries out a planned relocation: the directory first, then the branch, putting the directory back when the branch cannot follow.
func (w *Worktreer) Relocate(ctx context.Context, plan Relocation) error {
	if plan.moveDir {
		if err := w.moveDir(ctx, plan, plan.FromPath, plan.ToPath); err != nil {
			return err
		}
	}
	if !plan.moveBranch {
		return nil
	}
	out, err := exec.CommandContext(ctx, "git", "-C", plan.Repo, "branch", "-m", plan.FromBranch, plan.ToBranch).CombinedOutput()
	if err == nil {
		return nil
	}
	renameErr := NewFailure(w.messages, clitext.WorktreeRelocateFailed, clitext.Params{clitext.ParamName: strconv.Quote(plan.Name), clitext.ParamOutput: redact.Output(out), clitext.ParamErr: err}, err)
	if plan.moveDir {
		return errors.Join(renameErr, w.moveDir(context.WithoutCancel(ctx), plan, plan.ToPath, plan.FromPath))
	}
	return renameErr
}

func (w *Worktreer) moveDir(ctx context.Context, plan Relocation, source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), relocatedDirMode); err != nil {
		return NewFailure(w.messages, clitext.WorktreeRelocateFailed, clitext.Params{clitext.ParamName: strconv.Quote(plan.Name), clitext.ParamOutput: "", clitext.ParamErr: err}, err)
	}
	if plan.Scratch {
		if err := os.Rename(source, target); err != nil {
			return NewFailure(w.messages, clitext.WorktreeRelocateFailed, clitext.Params{clitext.ParamName: strconv.Quote(plan.Name), clitext.ParamOutput: "", clitext.ParamErr: err}, err)
		}
		return nil
	}
	out, err := exec.CommandContext(ctx, "git", "-C", plan.Repo, subWorktree, "move", source, target).CombinedOutput()
	if err != nil {
		return NewFailure(w.messages, clitext.WorktreeRelocateFailed, clitext.Params{clitext.ParamName: strconv.Quote(plan.Name), clitext.ParamOutput: redact.Output(out), clitext.ParamErr: err}, err)
	}
	return nil
}

func exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// InBareLayout reports whether path is the worktree, or the scratch directory, a daemon without an instance id gave the session name.
func InBareLayout(name, path string, scratch bool) bool {
	layoutDir := sessionsDir
	if scratch {
		layoutDir = scratchDir
	}
	return path != "" && filepath.Base(path) == name && filepath.Base(filepath.Dir(filepath.Clean(path))) == layoutDir
}
