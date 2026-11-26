package host

import (
	"context"
	"os"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

func planLocalBareRelocation(ctx context.Context, worktrees *worktree.Worktreer, creds contracts.ForgeCredentials, repo string, bare worktree.BareSession) (worktree.Relocation, error) {
	ctx, cancel := context.WithTimeout(ctx, sessionPRForgeLimit)
	defer cancel()
	bare.Origin = migrationOriginAccess(ctx, creds, repo, bare.PullRequest)
	return worktrees.PlanBareRelocation(ctx, bare)
}

func migrationOriginAccess(ctx context.Context, creds contracts.ForgeCredentials, repo string, pullRequest int) worktree.OriginAccess {
	var access worktree.OriginAccess
	if pullRequest == 0 {
		return access
	}
	if origin, err := prOriginRepo(ctx, repo); err == nil {
		access.Env, err = forge.GitEnvironment(ctx, creds, origin.Host())
		if err != nil {
			return worktree.OriginAccess{Unavailable: err}
		}
	}
	if !sshCommandChosen(ctx, repo) {
		access.Env = append(access.Env, "GIT_SSH_COMMAND="+runner.GitSSHCommand)
	}
	return access
}

func sshCommandChosen(ctx context.Context, repo string) bool {
	if os.Getenv("GIT_SSH_COMMAND") != "" || os.Getenv("GIT_SSH") != "" {
		return true
	}
	configured, err := prGit(ctx, repo, nil, "config", "--get", "core.sshCommand")
	return err == nil && configured != ""
}
