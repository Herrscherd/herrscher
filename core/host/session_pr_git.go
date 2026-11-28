package host

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/runner"
)

const (
	gitHubHostName = "github.com"
	originRemote   = "origin"
	gitMissingKey  = 1
)

type prOriginError struct {
	dir   string
	cause error
}

func (e prOriginError) Error() string { return e.cause.Error() }

func (e prOriginError) Unwrap() error { return e.cause }

func prOriginRepo(ctx context.Context, dir string) (contracts.ForgeRepoRef, error) {
	raw, err := prGit(ctx, dir, nil, "config", "--get", "remote."+originRemote+".url")
	if err != nil {
		return contracts.ForgeRepoRef{}, prOriginError{dir: dir, cause: missingOriginCause(err)}
	}
	if raw == "" {
		return contracts.ForgeRepoRef{}, prOriginError{dir: dir, cause: errPRNoOrigin}
	}
	repo, err := contracts.ParseRepoRef(raw)
	if err != nil {
		return contracts.ForgeRepoRef{}, prOriginError{dir: dir, cause: err}
	}
	if repo.Host().String() != gitHubHostName {
		return contracts.ForgeRepoRef{}, contracts.ErrForgeWrongHost
	}
	return repo, nil
}

func missingOriginCause(err error) error {
	var exited *exec.ExitError
	if errors.As(err, &exited) && exited.ExitCode() == gitMissingKey {
		return errPRNoOrigin
	}
	return err
}

func branchPushed(ctx context.Context, dir, branch string, env []string) (bool, error) {
	head, err := prGit(ctx, dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	remote, err := prGit(ctx, dir, env, "ls-remote", "--heads", originRemote, "refs/heads/"+branch)
	if err != nil {
		return false, err
	}
	remoteHead, _, _ := strings.Cut(remote, "\t")
	return remoteHead != "" && remoteHead == head, nil
}

func defaultBranch(ctx context.Context, dir string) (string, error) {
	ref, err := prGit(ctx, dir, nil, "symbolic-ref", "--quiet", "--short", "refs/remotes/"+originRemote+"/HEAD")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", errPRNoBase
	}
	if ref == "" {
		return "", errPRNoBase
	}
	return strings.TrimPrefix(ref, originRemote+"/"), nil
}

type gitRunner func(ctx context.Context, dir string, env []string, args ...string) (string, error)

func prGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := runner.Local{}.Command(ctx, dir, append([]string{"git"}, args...)...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.Output()
	if err != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	return strings.TrimSpace(string(out)), err
}
