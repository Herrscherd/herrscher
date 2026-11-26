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

type dirPlan int

const (
	dirMissing dirPlan = iota
	dirToMove
	dirMoved
)

// Relocation moves one session out of the layout a daemon without an instance id used.
type Relocation struct {
	Name       string
	Repo       string
	FromPath   string
	ToPath     string
	FromBranch string
	ToBranch   string
	Scratch    bool
	dir        dirPlan
	moveBranch bool
}

// MovesDir reports whether carrying out the relocation moves the directory.
func (r Relocation) MovesDir() bool { return r.dir == dirToMove }

// RenamesBranch reports whether carrying out the relocation renames the branch.
func (r Relocation) RenamesBranch() bool { return r.moveBranch }

// DirAtTarget reports whether the directory sits at ToPath once the relocation is carried out.
func (r Relocation) DirAtTarget() bool { return r.dir != dirMissing }

// BareSession is a session of the id-less layout as the state records it.
type BareSession struct {
	Name        string
	Path        string
	Scratch     bool
	PullRequest int
	Origin      OriginAccess
}

// BareTarget reports where a session of the id-less layout belongs under this instance, without looking at the disk.
func (w *Worktreer) BareTarget(sess BareSession) (Relocation, error) {
	if !InBareLayout(sess.Path, sess.Scratch) {
		return Relocation{}, NewFailure(w.messages, clitext.WorktreeNotAWorktree, clitext.Params{clitext.ParamPath: strconv.Quote(sess.Path)}, nil)
	}
	from := filepath.Clean(sess.Path)
	for _, taken := range []string{sess.Name, filepath.Base(from)} {
		if taken == w.instanceID {
			return Relocation{}, NewFailure(w.messages, clitext.WorktreeRelocateNameIsInstance, clitext.Params{clitext.ParamName: strconv.Quote(sess.Name), clitext.ParamValue: strconv.Quote(w.instanceID), clitext.ParamPath: strconv.Quote(from)}, nil)
		}
	}
	repo := filepath.Dir(filepath.Dir(from))
	plan := Relocation{Name: sess.Name, Repo: repo, FromPath: from, Scratch: sess.Scratch, dir: dirToMove}
	if sess.Scratch {
		plan.ToPath = w.ScratchPath(repo, sess.Name)
		return plan, nil
	}
	plan.ToPath = w.Path(repo, sess.Name)
	plan.FromBranch, plan.ToBranch = branchPrefix+filepath.Base(from), w.Branch(sess.Name)
	plan.moveBranch = true
	return plan, nil
}

// PlanBareRelocation reports what moving a session out of the id-less layout still has to do on this machine, refusing what it cannot decide alone. A worktree that lies in no git repository, or whose repository is gone, is left where it is.
func (w *Worktreer) PlanBareRelocation(ctx context.Context, sess BareSession) (Relocation, error) {
	plan, err := w.BareTarget(sess)
	if err != nil {
		return Relocation{}, err
	}
	plan.dir, err = w.planDirMove(plan)
	if err != nil {
		return Relocation{}, err
	}
	if sess.Scratch {
		return plan, nil
	}
	gone, err := w.outsideAnyRepository(ctx, plan)
	if err != nil {
		return Relocation{}, err
	}
	if gone {
		plan.dir, plan.moveBranch = dirMissing, false
		return plan, nil
	}
	if plan.dir == dirToMove {
		if branchErr := w.checkOnBranch(ctx, plan.FromPath, plan.FromBranch); branchErr != nil {
			return Relocation{}, branchErr
		}
	}
	plan.moveBranch, err = w.planBranchRename(ctx, plan, sess)
	if err != nil {
		return Relocation{}, err
	}
	return plan, nil
}

func (w *Worktreer) planDirMove(plan Relocation) (dirPlan, error) {
	fromExists, err := exists(plan.FromPath)
	if err != nil {
		return dirMissing, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(plan.FromPath), clitext.ParamErr: err}, err)
	}
	toExists, err := exists(plan.ToPath)
	if err != nil {
		return dirMissing, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(plan.ToPath), clitext.ParamErr: err}, err)
	}
	switch {
	case fromExists && toExists:
		return dirMissing, NewFailure(w.messages, clitext.WorktreeRelocateTargetExists, clitext.Params{clitext.ParamName: strconv.Quote(plan.Name), clitext.ParamPath: strconv.Quote(plan.ToPath), clitext.ParamFrom: strconv.Quote(plan.FromPath)}, nil)
	case fromExists:
		return dirToMove, nil
	case toExists:
		return dirMoved, nil
	}
	return dirMissing, nil
}

func (w *Worktreer) outsideAnyRepository(ctx context.Context, plan Relocation) (bool, error) {
	var probe string
	switch plan.dir {
	case dirMoved:
		return false, nil
	case dirToMove:
		probe = plan.FromPath
	case dirMissing:
		probe = plan.Repo
		present, err := exists(probe)
		if err != nil {
			return false, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(probe), clitext.ParamErr: err}, err)
		}
		if !present {
			return true, nil
		}
	}
	outside, err := OutsideAnyRepository(ctx, probe)
	if err != nil {
		return false, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(probe), clitext.ParamErr: err}, err)
	}
	return outside, nil
}

func (w *Worktreer) checkOnBranch(ctx context.Context, path, branch string) error {
	tip, err := w.BranchAt(ctx, path)
	if err != nil {
		return err
	}
	if tip != branch {
		return NewFailure(w.messages, clitext.WorktreeOtherBranch, clitext.Params{clitext.ParamPath: strconv.Quote(path), clitext.ParamTip: strconv.Quote(tip), clitext.ParamBranch: strconv.Quote(branch)}, nil)
	}
	return nil
}

func (w *Worktreer) planBranchRename(ctx context.Context, plan Relocation, sess BareSession) (bool, error) {
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
	remote, err := w.pushedAs(ctx, plan, sess)
	if err != nil {
		return false, err
	}
	if remote != "" {
		return false, NewFailure(w.messages, clitext.WorktreeRelocateRemoteBranch, clitext.Params{clitext.ParamName: strconv.Quote(plan.Name), clitext.ParamBranch: plan.FromBranch, clitext.ParamTarget: remote}, nil)
	}
	return true, nil
}

// Relocate carries out a planned relocation: the directory first, then the branch, putting the directory back when the branch cannot follow.
func (w *Worktreer) Relocate(ctx context.Context, plan Relocation) error {
	if plan.MovesDir() {
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
	if plan.MovesDir() {
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

// InBareLayout reports whether path is a session worktree, or scratch directory, of the layout a daemon without an instance id used, whatever the session is called now.
func InBareLayout(path string, scratch bool) bool {
	layoutDir := sessionsDir
	if scratch {
		layoutDir = scratchDir
	}
	return path != "" && filepath.Base(filepath.Dir(filepath.Clean(path))) == layoutDir
}

// LayoutRepository reports the repository a session worktree of either layout lives in, read from its path alone, so it answers for a worktree that is gone.
func LayoutRepository(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	parent := filepath.Dir(filepath.Clean(path))
	for _, dir := range []string{parent, filepath.Dir(parent)} {
		if filepath.Base(dir) == sessionsDir {
			return filepath.Dir(dir), true
		}
	}
	return "", false
}
