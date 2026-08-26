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
		return fmt.Errorf("worktree: want one of create, pre-existing, remove, materialize")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("worktree "+sub, flag.ContinueOnError)
	repo := fs.String("repo", "", "repository root")
	name := fs.String("name", "", "logical session name")
	instance := fs.String("instance", "", "daemon instance id (namespaces the layout)")
	base := fs.String("base", "", "ref the new branch starts at (create only)")
	force := fs.Bool("force", false, "remove even with local changes (remove only)")
	worktreePath := fs.String("worktree", "", "worktree to extract into (materialize only)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if sub != "materialize" && (*repo == "" || *name == "") {
		return fmt.Errorf("worktree %s: --repo and --name are required", sub)
	}
	wt := worktree.NewWorktreer(ctx, *instance)
	switch sub {
	case "create":
		path, err := wt.Create(*repo, *name, *base)
		if err != nil {
			return err
		}
		// An empty path is not an error: it is the non-git fallback, and the
		// caller falls back to a shared session exactly as a local one does.
		return emitJSON(map[string]string{"path": path})
	case "pre-existing":
		return emitJSON(map[string]bool{"preExisting": wt.PreExisting(*repo, *name)})
	case "remove":
		if err := wt.Remove(*repo, *name, *force); err != nil {
			return err
		}
		return emitJSON(map[string]bool{"removed": true})
	case "materialize":
		// Reads a tar of an agent's provisioning files on stdin, from the daemon
		// on the other machine. The excludes run here, after the write, because
		// they touch the repository, and the repository is on this side.
		if *worktreePath == "" {
			return fmt.Errorf("worktree materialize: --worktree is required")
		}
		if err := extractTar(os.Stdin, *worktreePath); err != nil {
			return err
		}
		if err := agent.EnsureGitExcludes(*worktreePath); err != nil {
			return err
		}
		return emitJSON(map[string]bool{"materialized": true})
	default:
		return fmt.Errorf("worktree: unknown subcommand %q (want create, pre-existing, remove or materialize)", sub)
	}
}

// emitJSON prints one JSON object on stdout, the whole of this verb's output.
// The caller is a program, so there is nothing else on the stream.
func emitJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(b))
	return nil
}
