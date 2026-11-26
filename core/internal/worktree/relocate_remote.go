package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/redact"
	"github.com/Herrscherd/herrscher/core/internal/shellquote"
)

const (
	pullRequestRemote  = "origin"
	defaultOriginLimit = time.Minute
	originWaitDelay    = 2 * time.Second
)

// OriginAccess is how the migration asks the pull request remote whether it still holds a branch: the environment git runs with, or why origin cannot be asked at all.
type OriginAccess struct {
	Env         []string
	Unavailable error
}

func (w *Worktreer) pushedAs(ctx context.Context, plan Relocation, sess BareSession) (string, error) {
	remotes, err := w.remotes(ctx, plan.Repo)
	if err != nil {
		return "", err
	}
	tracked, err := w.remoteRefs(ctx, plan.Repo)
	if err != nil {
		return "", err
	}
	for _, remote := range remotes {
		if tracked[remote+"/"+plan.FromBranch] {
			return remote, nil
		}
	}
	if sess.PullRequest == 0 || !slices.Contains(remotes, pullRequestRemote) {
		return "", nil
	}
	return w.originHolding(ctx, plan, sess.Origin)
}

func (w *Worktreer) remotes(ctx context.Context, repo string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "remote").Output()
	if err != nil {
		return nil, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(repo), clitext.ParamErr: err}, err)
	}
	return strings.Fields(string(out)), nil
}

func (w *Worktreer) remoteRefs(ctx context.Context, repo string) (map[string]bool, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "for-each-ref", "--format=%(refname:lstrip=2)", "refs/remotes").Output()
	if err != nil {
		return nil, NewFailure(w.messages, clitext.WorktreeInspect, clitext.Params{clitext.ParamPath: strconv.Quote(repo), clitext.ParamErr: err}, err)
	}
	refs := make(map[string]bool)
	for ref := range strings.Lines(string(out)) {
		refs[strings.TrimSpace(ref)] = true
	}
	return refs, nil
}

func (w *Worktreer) originHolding(ctx context.Context, plan Relocation, access OriginAccess) (string, error) {
	if access.Unavailable != nil {
		return "", w.originUnreachable(plan, nil, access.Unavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, defaultOriginLimit)
	defer cancel()
	lsRemote := exec.CommandContext(ctx, "git", "-C", plan.Repo, "ls-remote", "--heads", pullRequestRemote, "refs/heads/"+plan.FromBranch)
	lsRemote.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS="), access.Env...)
	lsRemote.WaitDelay = originWaitDelay
	heads, err := lsRemote.CombinedOutput()
	if err != nil {
		return "", w.originUnreachable(plan, heads, err)
	}
	if strings.TrimSpace(string(heads)) != "" {
		return pullRequestRemote, nil
	}
	return "", nil
}

func (w *Worktreer) originUnreachable(plan Relocation, output []byte, err error) error {
	return NewFailure(w.messages, clitext.WorktreeRelocateRemoteUnreachable, clitext.Params{
		clitext.ParamName:    strconv.Quote(plan.Name),
		clitext.ParamBranch:  plan.FromBranch,
		clitext.ParamTarget:  pullRequestRemote,
		clitext.ParamOutput:  redact.Output(output),
		clitext.ParamErr:     err,
		clitext.ParamCommand: manualRelocation(plan),
	}, err)
}

func manualRelocation(plan Relocation) string {
	quote := shellquote.Quote
	branchRename := "git -C " + quote(plan.Repo) + " branch -m " + quote(plan.FromBranch) + " " + quote(plan.ToBranch)
	if !plan.MovesDir() {
		return branchRename
	}
	return "mkdir -p " + quote(filepath.Dir(plan.ToPath)) + " && git -C " + quote(plan.Repo) + " worktree move " + quote(plan.FromPath) + " " + quote(plan.ToPath) + " && " + branchRename
}
