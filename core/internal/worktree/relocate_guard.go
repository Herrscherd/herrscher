package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/shellquote"
)

func (w *Worktreer) checkInstanceFolderOnDisk(plan Relocation) error {
	fromExists, err := exists(plan.FromPath)
	if err != nil {
		return NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(plan.FromPath), clitext.ParamErr: err}, err)
	}
	if !fromExists {
		return nil
	}
	folder := filepath.Dir(plan.ToPath)
	info, err := os.Lstat(folder)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(folder), clitext.ParamErr: err}, err)
	}
	if !info.IsDir() {
		return w.instanceFolderTaken(plan)
	}
	gitEntry, err := exists(filepath.Join(folder, gitEntryName))
	if err != nil {
		return NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(folder), clitext.ParamErr: err}, err)
	}
	if gitEntry {
		return w.instanceFolderTaken(plan)
	}
	return nil
}

func (w *Worktreer) checkInstanceFolderUnregistered(ctx context.Context, plan Relocation) error {
	registered, err := w.registeredWorktree(ctx, plan.Repo, filepath.Dir(plan.ToPath))
	if err != nil {
		return err
	}
	if registered {
		return w.instanceFolderTaken(plan)
	}
	return nil
}

func (w *Worktreer) instanceFolderTaken(plan Relocation) error {
	return NewFailure(w.messages, clitext.WorktreeRelocateInstanceFolder, clitext.Params{
		clitext.ParamName:  strconv.Quote(plan.Name),
		clitext.ParamValue: strconv.Quote(w.instanceID),
		clitext.ParamPath:  strconv.Quote(filepath.Dir(plan.ToPath)),
	}, nil)
}

func (w *Worktreer) checkSameRepository(ctx context.Context, plan Relocation, worktreePath string) error {
	own, err := commonGitDir(ctx, worktreePath)
	if err != nil {
		return NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(worktreePath), clitext.ParamErr: err}, err)
	}
	expected, err := commonGitDir(ctx, plan.Repo)
	if err != nil {
		return NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(plan.Repo), clitext.ParamErr: err}, err)
	}
	if own == expected {
		return nil
	}
	params := clitext.Params{
		clitext.ParamName:   strconv.Quote(plan.Name),
		clitext.ParamPath:   strconv.Quote(worktreePath),
		clitext.ParamTarget: strconv.Quote(repositoryOfGitDir(own)),
		clitext.ParamRepo:   strconv.Quote(plan.Repo),
	}
	adminDir, err := adminDirIn(ctx, worktreePath, own, expected)
	if err != nil {
		return NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(worktreePath), clitext.ParamErr: err}, err)
	}
	if adminDir == "" {
		return NewFailure(w.messages, clitext.WorktreeRelocateForeignUnlinked, params, nil)
	}
	params[clitext.ParamCommand] = relinkCommand(worktreePath, adminDir)
	return NewFailure(w.messages, clitext.WorktreeRelocateForeignRepository, params, nil)
}

func adminDirIn(ctx context.Context, worktreePath, ownCommon, expectedCommon string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", worktreePath, "rev-parse", "--path-format=absolute", "--git-dir").Output()
	if err != nil {
		return "", err
	}
	gitDir := filepath.Clean(strings.TrimSpace(string(out)))
	if resolved, resolveErr := filepath.EvalSymlinks(gitDir); resolveErr == nil {
		gitDir = resolved
	}
	if filepath.Dir(gitDir) != filepath.Join(ownCommon, worktreesDirName) {
		return "", nil
	}
	adminDir := filepath.Join(expectedCommon, worktreesDirName, filepath.Base(gitDir))
	info, err := os.Lstat(adminDir)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", nil
	}
	return adminDir, nil
}

func relinkCommand(worktreePath, adminDir string) string {
	gitEntry := filepath.Join(worktreePath, gitEntryName)
	return "printf '%s\\n' " + shellquote.Quote("gitdir: "+adminDir) + " > " + shellquote.Quote(gitEntry) +
		" && printf '%s\\n' " + shellquote.Quote(gitEntry) + " > " + shellquote.Quote(filepath.Join(adminDir, adminGitdirName))
}

func commonGitDir(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", err
	}
	common := filepath.Clean(strings.TrimSpace(string(out)))
	if resolved, resolveErr := filepath.EvalSymlinks(common); resolveErr == nil {
		return resolved, nil
	}
	return common, nil
}

func repositoryOfGitDir(gitDir string) string {
	if filepath.Base(gitDir) == gitEntryName {
		return filepath.Dir(gitDir)
	}
	return gitDir
}

func (w *Worktreer) checkMovedBranch(ctx context.Context, plan Relocation) error {
	tip, err := w.BranchAt(ctx, plan.ToPath)
	if err != nil {
		return err
	}
	if tip != plan.FromBranch && tip != plan.ToBranch {
		return NewFailure(w.messages, clitext.WorktreeOtherBranch, clitext.Params{clitext.ParamPath: strconv.Quote(plan.ToPath), clitext.ParamTip: strconv.Quote(tip), clitext.ParamBranch: strconv.Quote(plan.ToBranch)}, nil)
	}
	present, err := w.BranchExistsAt(ctx, plan.Repo, tip)
	if err != nil || present {
		return err
	}
	renamed := plan.ToBranch
	if tip == plan.ToBranch {
		renamed = plan.FromBranch
	}
	renamedPresent, err := w.BranchExistsAt(ctx, plan.Repo, renamed)
	if err != nil {
		return err
	}
	params := clitext.Params{
		clitext.ParamName:   strconv.Quote(plan.Name),
		clitext.ParamPath:   strconv.Quote(plan.ToPath),
		clitext.ParamTip:    strconv.Quote(tip),
		clitext.ParamBranch: strconv.Quote(renamed),
	}
	if !renamedPresent {
		return NewFailure(w.messages, clitext.WorktreeRelocateBranchesGone, params, nil)
	}
	params[clitext.ParamCommand] = "git -C " + shellquote.Quote(plan.ToPath) + " symbolic-ref HEAD " + shellquote.Quote("refs/heads/"+renamed)
	return NewFailure(w.messages, clitext.WorktreeRelocateHeadGone, params, nil)
}
