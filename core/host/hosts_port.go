package host

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

// hostPlacer is the composition root's answer to "where does this session run".
// It lives here rather than in the manager because it is the only piece that
// needs both the host records and the runner.
type hostPlacer struct {
	st         *state.State
	instanceID string
	local      *worktree.Worktreer
	// sourceVersion is what the daemon would provision today. A seam so the
	// drift guard is testable without a git checkout.
	sourceVersion func() string
}

func (p hostPlacer) resolve(name string) (state.Host, *worktree.Remote, error) {
	h, ok := p.st.FindHost(name)
	if !ok {
		return state.Host{}, nil, unknownHost(p.st, name)
	}
	if h.Bin == "" {
		return state.Host{}, nil, fmt.Errorf("host %q is not provisioned: run `host provision %s`", name, name)
	}
	r := runner.SSH{Target: h.SSH, ControlPath: runner.ControlPathFor(h.SSH)}
	return h, worktree.NewRemote(r, h.Bin, p.instanceID), nil
}

// Ready says whether a host can carry a new session.
//
// Version drift is the dangerous case, not a cosmetic one: the local supervisor
// builds the bridge argv, so a bridge older than this daemon dies on an unknown
// flag the instant it starts, and the backoff would restart it forever with
// nobody able to see why.
//
// It is asked when a session is created, and nowhere else. Refusing to close a
// session because the source moved since it started would strand it: the bridge
// is already stopped by then, and removing a worktree over there asks nothing of
// the remote version.
//
// An unknown source version is not a drift. A daemon with no source checkout has
// nothing to compare against, and refusing every session would be worse than
// running one that may well be current.
func (p hostPlacer) Ready(name string) error {
	if name == "" {
		return nil
	}
	h, _, err := p.resolve(name)
	if err != nil {
		return err
	}
	if p.sourceVersion == nil {
		return nil
	}
	if want := p.sourceVersion(); want != "" && h.Version != want {
		return fmt.Errorf("host %q runs herrscher %s but this daemon builds %s: run `host provision %s`", name, h.Version, want, name)
	}
	return nil
}

// Worktrees returns the worktree lifecycle for a host. An empty name is this
// machine, answered by the local Worktreer with no subprocess at all.
func (p hostPlacer) Worktrees(name string) (manager.Worktrees, error) {
	if name == "" {
		return p.local, nil
	}
	_, rem, err := p.resolve(name)
	if err != nil {
		return nil, err
	}
	return rem, nil
}

func (p hostPlacer) Workspace(name string) (string, error) {
	if name == "" {
		return p.st.WorkspaceRoot(), nil
	}
	h, _, err := p.resolve(name)
	if err != nil {
		return "", err
	}
	return h.Workspace, nil
}

func (p hostPlacer) Materialize(ctx context.Context, name string, a agent.Agent, worktreePath string, hook bool) error {
	if name == "" {
		return a.MaterializeAs(worktreePath, binIf(hook, agent.SelfBin()))
	}
	h, rem, err := p.resolve(name)
	if err != nil {
		return err
	}
	// h.Bin is herrscher over there, the same binary the bridge is launched
	// with; a hook rendered with anything else would name a path that does not
	// exist on that machine.
	payload, err := stageAgentTar(a, worktreePath, binIf(hook, h.Bin))
	if err != nil {
		return err
	}
	return rem.Materialize(ctx, worktreePath, payload)
}

// binIf is the binary when the session wants a hook, and the empty string that
// means "no hook" when it does not.
func binIf(hook bool, bin string) string {
	if !hook {
		return ""
	}
	return bin
}

// EnsureProject clones the project on a host when it is not there yet, from the
// origin of the same project here. git's own message is surfaced as it comes:
// it is more precise than anything written on top of it would be.
func (p hostPlacer) EnsureProject(ctx context.Context, name, project, repo string) error {
	if name == "" || project == "" {
		return nil
	}
	h, _, err := p.resolve(name)
	if err != nil {
		return err
	}
	run := runnerFor(h)
	if _, err := capture(ctx, run, "test", "-d", repo+"/.git"); err == nil {
		return nil
	}
	origin, err := originOf(ctx, filepath.Join(p.st.WorkspaceRoot(), project))
	if err != nil {
		return fmt.Errorf("cannot clone %s onto %s: %w", project, name, err)
	}
	if _, err := capture(ctx, run, "git", "clone", origin, repo); err != nil {
		return err
	}
	return nil
}

// originOf reads the origin remote of a local project, which is what a host is
// told to clone from.
func originOf(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("read origin of %s: %s", dir, msg)
	}
	url := strings.TrimSpace(stdout.String())
	if url == "" {
		return "", fmt.Errorf("%s has no origin remote", dir)
	}
	return url, nil
}
