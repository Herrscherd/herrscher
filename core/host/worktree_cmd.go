package host

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

// RunWorktree is the machine-facing façade over the worktree manager. It exists
// so a daemon can drive worktree lifecycle on another machine: the manager's
// work is filesystem work (Lstat, ReadDir, symlink resolution), so it belongs
// where the filesystem is, and the herrscher binary is already over there.
//
// Answered locally rather than forwarded to a daemon, for the same reason
// whoami is: there is no daemon on the far side, and the caller of this verb is
// a daemon on another machine that reads its JSON.
func RunWorktree(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errWorktreeSubcommand
	}
	flags, err := parseWorktreeFlags(args[0], args[1:])
	if err != nil {
		return err
	}
	if flags.sub != "materialize" && (flags.repo == "" || flags.name == "") {
		return &worktreeFlagsError{Sub: flags.sub}
	}
	return runWorktreeSub(ctx, flags)
}

type worktreeFlags struct {
	sub          string
	repo         string
	name         string
	instance     string
	base         string
	force        bool
	worktreePath string
}

func parseWorktreeFlags(sub string, rest []string) (worktreeFlags, error) {
	flags := flag.NewFlagSet("worktree "+sub, flag.ContinueOnError)
	repo := flags.String("repo", "", "repository root")
	name := flags.String(paramName, "", "logical session name")
	instance := flags.String("instance", "", "daemon instance id (namespaces the layout)")
	base := flags.String("base", "", "ref the new branch starts at (create only)")
	force := flags.Bool("force", false, "remove even with local changes (remove only)")
	worktreePath := flags.String("worktree", "", "worktree to extract into (materialize only)")
	if err := flags.Parse(rest); err != nil {
		return worktreeFlags{}, err
	}
	return worktreeFlags{
		sub:          sub,
		repo:         *repo,
		name:         *name,
		instance:     *instance,
		base:         *base,
		force:        *force,
		worktreePath: *worktreePath,
	}, nil
}

func runWorktreeSub(ctx context.Context, file worktreeFlags) error {
	worktrees := worktree.NewWorktreer(file.instance)
	switch file.sub {
	case "create":
		return createWorktree(ctx, worktrees, file)
	case "pre-existing":
		return emitJSON(map[string]bool{"preExisting": worktrees.PreExisting(ctx, file.repo, file.name)})
	case "remove":
		return removeWorktree(ctx, worktrees, file)
	case "materialize":
		return materializeWorktree(ctx, file.worktreePath)
	default:
		return &unknownWorktreeSubcommandError{Sub: file.sub}
	}
}

func createWorktree(ctx context.Context, wt *worktree.Worktreer, f worktreeFlags) error {
	path, err := wt.Create(ctx, f.repo, f.name, f.base)
	if err != nil {
		return err
	}
	return emitJSON(map[string]string{"path": path})
}

func removeWorktree(ctx context.Context, wt *worktree.Worktreer, f worktreeFlags) error {
	if err := wt.Remove(ctx, f.repo, f.name, f.force); err != nil {
		return err
	}
	return emitJSON(map[string]bool{"removed": true})
}

func materializeWorktree(ctx context.Context, worktreePath string) error {
	if worktreePath == "" {
		return errWorktreeMaterializeRequired
	}
	if err := extractTar(os.Stdin, worktreePath); err != nil {
		return err
	}
	if err := agent.EnsureGitExcludes(ctx, worktreePath); err != nil {
		return err
	}
	return emitJSON(map[string]bool{"materialized": true})
}

func emitJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	logWriteFailure(fmt.Fprintln(os.Stdout, string(payload)))
	return nil
}
